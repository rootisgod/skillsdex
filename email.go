package skillsdex

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/resend/resend-go/v3"
)

const (
	envResendAPIKey = "SKILLSDEX_RESEND_API_KEY"
	envEmailFrom    = "SKILLSDEX_EMAIL_FROM"
	envEmailReplyTo = "SKILLSDEX_EMAIL_REPLY_TO"
)

type inviteEmailSender interface {
	SendInvite(ctx context.Context, to, inviteURL string, expiresAt time.Time) (string, error)
}

type resendInviteEmailSender struct {
	client  *resend.Client
	from    string
	replyTo string
}

func inviteEmailSenderFromEnv() (inviteEmailSender, error) {
	apiKey := strings.TrimSpace(os.Getenv(envResendAPIKey))
	from := strings.TrimSpace(os.Getenv(envEmailFrom))
	replyTo := strings.TrimSpace(os.Getenv(envEmailReplyTo))
	if apiKey == "" && from == "" && replyTo == "" {
		return nil, nil
	}
	if apiKey == "" {
		return nil, fmt.Errorf("%s is required when invite email is configured", envResendAPIKey)
	}
	if from == "" {
		return nil, fmt.Errorf("%s is required when invite email is configured", envEmailFrom)
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("%s is invalid: %w", envEmailFrom, err)
	}
	if replyTo != "" {
		if err := validateInviteEmailAddress(replyTo); err != nil {
			return nil, fmt.Errorf("%s is invalid: %w", envEmailReplyTo, err)
		}
	}
	return newResendInviteEmailSender(apiKey, from, replyTo), nil
}

func newResendInviteEmailSender(apiKey, from, replyTo string) *resendInviteEmailSender {
	client := resend.NewCustomClient(&http.Client{Timeout: 15 * time.Second}, apiKey)
	baseURL, _ := url.Parse("https://api.resend.com/")
	client.BaseURL = baseURL
	return &resendInviteEmailSender{client: client, from: from, replyTo: replyTo}
}

func (s *resendInviteEmailSender) SendInvite(ctx context.Context, to, inviteURL string, expiresAt time.Time) (string, error) {
	if err := validateInviteEmailAddress(to); err != nil {
		return "", err
	}
	if strings.TrimSpace(inviteURL) == "" {
		return "", errors.New("invite URL is required")
	}
	params := &resend.SendEmailRequest{
		From:    s.from,
		To:      []string{strings.TrimSpace(to)},
		Subject: "Your SkillsDex invite",
		Html:    inviteEmailHTML(inviteURL, expiresAt),
		Text:    inviteEmailText(inviteURL, expiresAt),
		Tags: []resend.Tag{
			{Name: "type", Value: "signup_invite"},
		},
	}
	if s.replyTo != "" {
		params.ReplyTo = s.replyTo
	}
	sent, err := s.client.Emails.SendWithContext(ctx, params)
	if err != nil {
		return "", err
	}
	if sent == nil || strings.TrimSpace(sent.Id) == "" {
		return "", errors.New("resend did not return an email id")
	}
	return sent.Id, nil
}

func validateInviteEmailAddress(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("email is required")
	}
	address, err := mail.ParseAddress(value)
	if err != nil {
		return err
	}
	if address.Address != value {
		return errors.New("use a single plain email address")
	}
	return nil
}

func inviteEmailHTML(inviteURL string, expiresAt time.Time) string {
	escapedURL := html.EscapeString(inviteURL)
	escapedExpiry := html.EscapeString(expiresAt.Format("2006-01-02 15:04 MST"))
	return `<p>You have been invited to join SkillsDex.</p>` +
		`<p><a href="` + escapedURL + `">Create your account</a></p>` +
		`<p>This invite expires at ` + escapedExpiry + ` and can be used once.</p>`
}

func inviteEmailText(inviteURL string, expiresAt time.Time) string {
	return fmt.Sprintf("You have been invited to join SkillsDex.\n\nCreate your account: %s\n\nThis invite expires at %s and can be used once.\n", inviteURL, expiresAt.Format("2006-01-02 15:04 MST"))
}
