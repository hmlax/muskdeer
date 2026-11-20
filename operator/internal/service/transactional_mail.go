package service

import (
	"context"
	"errors"

	"github.com/tx7do/go-utils/mailer"
	"github.com/tx7do/go-utils/notify"
	"github.com/tx7do/go-utils/notify/sms"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
)

const (
	errorVerificationDelivery = "send verification message failed"
	emailNotConfigured        = "email delivery is not configured"
	smsNotConfigured          = "sms delivery is not configured"
)

// TransactionMailer sends authentication codes using deployment configuration.
// It is independent of application notifications and tenant business data.
type TransactionMailer struct {
	settings runtimeconfig.Settings
}

func NewTransactionMailer(ctx *bootstrap.Context) *TransactionMailer {
	return &TransactionMailer{
		settings: runtimeconfig.SettingsFromContext(ctx),
	}
}

func (m *TransactionMailer) sendEmail(ctx context.Context, target, title, body string) error {
	if m == nil || m.settings.SMTP.Host == "" || m.settings.SMTP.Port == 0 {
		return errors.New(emailNotConfigured)
	}
	cfg := m.settings.SMTP
	return mailer.SendMail(ctx, mailer.SmtpConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Username: cfg.Username,
		Password: cfg.Password,
		From:     cfg.From,
		FromName: cfg.FromName,
		TlsMode:  cfg.TLSMode,
	}, []string{target}, title, body)
}

func (m *TransactionMailer) sendSMS(ctx context.Context, target, code string) error {
	if m == nil || m.settings.SMS.Provider == "" {
		return errors.New(smsNotConfigured)
	}
	cfg := m.settings.SMS
	client, err := sms.New(nil, sms.Config{
		Provider:     cfg.Provider,
		AccessKeyID:  cfg.AccessKeyID,
		AccessSecret: cfg.AccessSecret,
		SignName:     cfg.SignName,
		TemplateCode: cfg.TemplateCode,
		AppID:        cfg.AppID,
		Region:       cfg.Region,
	})
	if err != nil {
		return err
	}
	if err := client.Precheck(); err != nil {
		return err
	}
	_, err = client.Send(ctx, &notify.Message{
		Target: target,
		Params: map[string]string{"code": code},
	})
	return err
}
