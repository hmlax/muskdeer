// Package adminactor carries admin's validated identity for business audit.
package adminactor

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

type actorKey struct{}

const (
	userHeader    = "muskdeer-actor-user"
	sessionHeader = "muskdeer-actor-session"
	nameHeader    = "muskdeer-actor-name"
	roleHeader    = "muskdeer-actor-role"
)

// Bind carries an audit snapshot; it never checks a session, role or CSRF proof.
func Bind(ctx context.Context, actor domain.AdminPrincipal) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// From returns only an identity bound by this process's trusted boundary.
func From(ctx context.Context) (domain.AdminPrincipal, bool) {
	if ctx == nil {
		return domain.AdminPrincipal{}, false
	}
	actor, ok := ctx.Value(actorKey{}).(domain.AdminPrincipal)
	return actor, ok
}

// Outgoing sends audit fields, never browser session tokens or CSRF secrets.
func Outgoing(ctx context.Context) context.Context {
	actor, ok := From(ctx)
	if !ok {
		return ctx
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(userHeader, strconv.FormatInt(actor.UserID, 10))
	md.Set(sessionHeader, strconv.FormatInt(actor.SessionID, 10))
	md.Set(nameHeader, actor.Username)
	md.Set(roleHeader, string(actor.Role))
	return metadata.NewOutgoingContext(ctx, md)
}

type registrar struct{ grpc.ServiceRegistrar }

// Registrar applies only to operator's management RPCs. TLS authenticates admin
// as a service. The actor is audit attribution, not a second permissions check.
func Registrar(owner grpc.ServiceRegistrar) grpc.ServiceRegistrar { return registrar{owner} }

func (r registrar) RegisterService(desc *grpc.ServiceDesc, implementation any) {
	copyDesc := *desc
	copyDesc.Methods = append([]grpc.MethodDesc(nil), desc.Methods...)
	for i := range copyDesc.Methods {
		original := copyDesc.Methods[i].Handler
		copyDesc.Methods[i].Handler = func(service any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			call, err := incoming(ctx)
			if err != nil {
				return nil, err
			}
			return original(service, call, decode, interceptor)
		}
	}
	r.ServiceRegistrar.RegisterService(&copyDesc, implementation)
}

func incoming(ctx context.Context) (context.Context, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceAdmin) {
		return nil, status.Error(codes.PermissionDenied, domain.MessageForbidden)
	}
	md, _ := metadata.FromIncomingContext(ctx)
	single := func(key string) string {
		values := md.Get(key)
		if len(values) != 1 {
			return ""
		}
		return values[0]
	}
	user, err := strconv.ParseInt(single(userHeader), 10, 64)
	session, sessionErr := strconv.ParseInt(single(sessionHeader), 10, 64)
	name, role := single(nameHeader), single(roleHeader)
	if err != nil || sessionErr != nil || user <= 0 || session <= 0 || strconv.FormatInt(user, 10) != single(userHeader) || strconv.FormatInt(session, 10) != single(sessionHeader) || name == "" || len(name) > 64 || !utf8.ValidString(name) || strings.ContainsAny(name, "\r\n\x00") || role == "" || len(role) > 32 {
		return nil, status.Error(codes.InvalidArgument, domain.MessageInvalidAuth)
	}
	return Bind(ctx, domain.AdminPrincipal{
		UserID:    user,
		SessionID: session,
		Username:  name,
		Role:      domain.AdminRole(role),
	}), nil
}
