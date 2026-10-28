// Command admin provisions an identity only after explicit local operator input.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/migrate"
	"github.com/hmlax/muskdeer/internal/repository"
)

type provisionReply struct {
	UserID   string           `json:"user_id"`
	Username string           `json:"username"`
	Role     domain.AdminRole `json:"role"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, args []string, output, diagnostic io.Writer) int {
	flags := flag.NewFlagSet("admin", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "local service configuration")
	username := flags.String("username", "", "explicit management username")
	role := flags.String("role", "", "viewer, operator or admin")
	passwordEnv := flags.String("password-env", "", "environment variable containing the new password")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *path == "" || *username == "" || !domain.AdminRole(*role).Allows(domain.AdminRole(*role)) || !environmentName(*passwordEnv) {
		return fail(diagnostic, "config, username, role and password-env are required", 2)
	}
	if runtime.Version() != "go1.27.3" {
		return fail(diagnostic, "Go 1.27.3 is required", 1)
	}
	password := os.Getenv(*passwordEnv)
	if password == "" {
		return fail(diagnostic, "the explicit password environment is empty", 2)
	}
	cfg, err := deer.LoadConfig(ctx, *path)
	if err != nil || cfg.Postgres == nil {
		return fail(diagnostic, "configuration loading failed", 1)
	}
	client, err := postgres.New(ctx, cfg.Postgres, "admin.bootstrap")
	if err != nil {
		return fail(diagnostic, "identity database opening failed", 1)
	}
	code := provision(ctx, client, *username, password, domain.AdminRole(*role), output, diagnostic)
	if err := client.Close(); err != nil {
		return fail(diagnostic, "identity database cleanup failed", 1)
	}
	return code
}

func provision(ctx context.Context, client *postgres.Client, username, password string, role domain.AdminRole, output, diagnostic io.Writer) int {
	db := client.GORM()
	if err := migrate.CheckCurrent(ctx, db); err != nil {
		return fail(diagnostic, "explicit migration is required before identity provisioning", 1)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		return fail(diagnostic, "identity initialization failed", 1)
	}
	principal, err := auth.Provision(ctx, "local-provisioner", username, password, role)
	if err != nil {
		return fail(diagnostic, "identity provisioning failed", 1)
	}
	if err := json.NewEncoder(output).Encode(provisionReply{
		UserID:   strconv.FormatInt(principal.UserID, 10),
		Username: principal.Username,
		Role:     principal.Role,
	}); err != nil {
		return 1
	}
	return 0
}

func environmentName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if c >= 'A' && c <= 'Z' || c == '_' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func fail(output io.Writer, message string, code int) int {
	if _, err := io.WriteString(output, message+"\n"); err != nil {
		return 1
	}
	return code
}
