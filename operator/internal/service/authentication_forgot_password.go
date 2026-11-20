package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tx7do/go-crud/viewer"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/deer/management/mailtext"
	"github.com/hmlax/muskdeer/deer/management/portalscope"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/notify/sms"
)

// generateVCode 生成 6 位数字验证码。
// 使用纳秒时间取模：验证码仅用于一次性校验，安全强度由
// 「单次有效 + 10 分钟 TTL + 服务端比对」保证，不依赖密码学随机。
func generateVCode() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

// resetIdentityTypeOf 按.identifier 的形态选凭证类型：像手机号（无 @ 且能归一）→ PHONE 走短信，
// 其余（邮箱/账号名）→ EMAIL 维持既有行为。防枚举口径两边一致：查无此人一律静默 200。
func resetIdentityTypeOf(identifier string) operatorV1.UserCredential_IdentityType {
	if !strings.Contains(identifier, "@") {
		if _, _, err := sms.NormalizePhoneNumber(identifier); err == nil {
			return operatorV1.UserCredential_PHONE
		}
	}
	return operatorV1.UserCredential_EMAIL
}

// ForgotPassword 忘记密码：向 identifier（已绑定的邮箱凭证或手机号凭证）发送
// 重置验证码——邮箱走 SMTP，手机号走 SMS（显式点名，不经路由表）。
// 用户不存在时同样返回成功，防止通过接口枚举有效账号。
func (s *AuthenticationService) ForgotPassword(ctx context.Context, req *operatorV1.ForgotPasswordRequest) (*emptypb.Empty, error) {
	ctx = viewer.WithSystemContext(ctx)
	identifier := strings.TrimSpace(req.GetIdentifier())
	if identifier == "" {
		return nil, operatorV1.ErrorAuthenticationBadRequest("identifier is required")
	}

	identityType := resetIdentityTypeOf(identifier)
	lookup := &operatorV1.GetUserCredentialByIdentifierRequest{
		IdentityType: identityType,
		Identifier:   identifier,
	}
	if scope, ok := portalscope.FromContext(ctx); ok {
		lookup.TenantId = &scope.TenantID
	}
	cred, credErr := s.userCredentialRepo.GetByIdentifier(ctx, lookup)
	userId := cred.GetUserId()
	if credErr != nil || userId == 0 {
		s.log.Infof(ctx, "forgot-password: no %s credential for [%s] (silent ok)", identityType.String(), identifier)
		return &emptypb.Empty{}, nil
	}

	code := generateVCode()
	if err := s.vcodeCache.Save(resetPasswordPurpose(ctx), identifier, code, 10*time.Minute); err != nil {
		return nil, operatorV1.ErrorAuthenticationInternalServerError("save verification code failed")
	}

	// 文案语言：优先收件用户的偏好 locale（个人中心设置），未设置回落请求
	// Accept-Language（mailtext.Resolve 的兜底链）。
	if user, uErr := s.userRepo.Get(ctx, &operatorV1.GetUserRequest{
		QueryBy: &operatorV1.GetUserRequest_Id{Id: userId},
	}); uErr == nil && user.GetLocale() != "" {
		if loc, ok := mailtext.LocaleOfTag(user.GetLocale()); ok {
			ctx = mailtext.WithLocale(ctx, loc)
		}
	}

	var sendErr error
	if identityType == operatorV1.UserCredential_PHONE {
		sendErr = s.mailer.sendSMS(ctx, identifier, code)
	} else {
		title, body := mailtext.PasswordResetCode(ctx, code)
		sendErr = s.mailer.sendEmail(ctx, identifier, title, body)
	}
	if sendErr != nil {
		s.log.Errorf(ctx, "forgot-password: verification delivery failed: %s", sendErr)
		return nil, operatorV1.ErrorAuthenticationInternalServerError(errorVerificationDelivery)
	}

	s.log.Infof(ctx, "forgot-password: reset code sent to [%s] for user [%d]", identifier, userId)
	return &emptypb.Empty{}, nil
}

// ResetPasswordByCode 凭邮箱验证码重置密码（免鉴权）。
// 校验通过后重置密码（密码策略/加密在 repo 层处理）并吊销该用户全部会话。
func (s *AuthenticationService) ResetPasswordByCode(ctx context.Context, req *operatorV1.ResetPasswordByCodeRequest) (*emptypb.Empty, error) {
	ctx = viewer.WithSystemContext(ctx)
	identifier := strings.TrimSpace(req.GetIdentifier())
	code := strings.TrimSpace(req.GetCode())
	newPassword := req.GetNewPassword()
	if identifier == "" || code == "" || newPassword == "" {
		return nil, operatorV1.ErrorAuthenticationBadRequest("identifier, code and new password are required")
	}

	if !s.vcodeCache.Verify(resetPasswordPurpose(ctx), identifier, code) {
		return nil, operatorV1.ErrorAuthenticationBadRequest("invalid or expired verification code")
	}

	lookup := &operatorV1.GetUserCredentialByIdentifierRequest{
		IdentityType: resetIdentityTypeOf(identifier),
		Identifier:   identifier,
	}
	if scope, ok := portalscope.FromContext(ctx); ok {
		lookup.TenantId = &scope.TenantID
	}
	cred, credErr := s.userCredentialRepo.GetByIdentifier(ctx, lookup)
	if credErr != nil || cred.GetUserId() == 0 {
		return nil, operatorV1.ErrorAuthenticationBadRequest("invalid or expired verification code")
	}
	userId := cred.GetUserId()

	user, err := s.userRepo.Get(ctx, &operatorV1.GetUserRequest{
		QueryBy: &operatorV1.GetUserRequest_Id{Id: userId},
	})
	if err != nil {
		return nil, err
	}

	if err := portalscope.CheckTenant(ctx, user.GetTenantId()); err != nil {
		return nil, err
	}

	if err = s.userCredentialRepo.ResetCredentialForUser(ctx, user.GetTenantId(), user.GetId(), &operatorV1.ResetCredentialRequest{
		IdentityType:  operatorV1.UserCredential_USERNAME,
		Identifier:    user.GetUsername(),
		NewCredential: newPassword,
		NeedDecrypt:   true,
	}); err != nil {
		s.log.Errorf(ctx, "reset password by code failed for user [%d]: %s", userId, err.Error())
		return nil, err
	}

	if err = s.authenticator.RevokeUserTokenAllClientTypes(ctx, userId); err != nil {
		s.log.Errorf(ctx, "revoke tokens after reset-by-code failed for user [%d]: %s", userId, err.Error())
	}

	s.log.Infof(ctx, "password reset by code done for user [%d]", userId)
	return &emptypb.Empty{}, nil
}

const resetPasswordCachePurpose = "reset_password"

func resetPasswordPurpose(ctx context.Context) string {
	if scope, ok := portalscope.FromContext(ctx); ok {
		return fmt.Sprintf("%s_%d", resetPasswordCachePurpose, scope.TenantID)
	}
	return resetPasswordCachePurpose
}
