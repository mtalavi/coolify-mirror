package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
)

// Notifications: the recovery key and the result of scheduled backups go to
// an email address and/or a Telegram chat. The mail server and the Telegram
// bot are the ones set in Coolify (Settings → Email, Notifications →
// Telegram) unless coolify-mirror has its own.

// NotifyConfig is where messages go.
type NotifyConfig struct {
	Email         string      `json:"email,omitempty"`          // recipient address
	SMTP          *SMTPConfig `json:"smtp,omitempty"`           // nil: Coolify's mail settings
	TelegramChat  string      `json:"telegram_chat,omitempty"`  // chat id (a person's id, a group's -100…)
	TelegramToken string      `json:"telegram_token,omitempty"` // "": Coolify's Telegram bot
	On            string      `json:"on,omitempty"`             // scheduled results: failure (default) | always | never
}

// SMTPConfig is a mail server of coolify-mirror's own.
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	Security string `json:"security,omitempty"` // starttls (default) | tls | none
}

// Channels says which channels are set.
func (n NotifyConfig) Channels() []string {
	var c []string
	if n.Email != "" {
		c = append(c, "email "+n.Email)
	}
	if n.TelegramChat != "" {
		c = append(c, "Telegram "+n.TelegramChat)
	}
	return c
}

// Wants reports whether a scheduled result is sent.
func (n NotifyConfig) Wants(ok bool) bool {
	switch n.On {
	case "never":
		return false
	case "always":
		return true
	}
	return !ok
}

// Notify sends a message to every channel set. It returns what was sent and
// what failed (a channel that fails never stops the others).
func Notify(ctx context.Context, in *coolify.Instance, n NotifyConfig, subject, body string) (sent []string, errs []error) {
	if n.Email != "" {
		if err := sendEmail(ctx, in, n, subject, body); err != nil {
			errs = append(errs, fmt.Errorf("email to %s: %w", n.Email, err))
		} else {
			sent = append(sent, "email "+n.Email)
		}
	}
	if n.TelegramChat != "" {
		if err := sendTelegram(ctx, in, n, subject+"\n\n"+body); err != nil {
			errs = append(errs, fmt.Errorf("Telegram %s: %w", n.TelegramChat, err))
		} else {
			sent = append(sent, "Telegram "+n.TelegramChat)
		}
	}
	return sent, errs
}

// coolifyMail is Coolify's own mail setup (instance settings).
type coolifyMail struct {
	SMTP   *SMTPConfig
	Resend string // API key
	From   string
}

func readCoolifyMail(ctx context.Context, in *coolify.Instance) (*coolifyMail, error) {
	if in == nil {
		return nil, errors.New("no Coolify here")
	}
	var rows []map[string]any
	if err := in.Query(ctx, `SELECT * FROM instance_settings ORDER BY id LIMIT 1`, &rows); err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("read Coolify's mail settings: %v", err)
	}
	r := rows[0]
	str := func(k string) string {
		v, _ := r[k].(string)
		return strings.TrimSpace(plainValue(in, v))
	}
	on := func(k string) bool { b, _ := r[k].(bool); return b }
	m := &coolifyMail{From: str("smtp_from_address")}
	if name := str("smtp_from_name"); name != "" && m.From != "" {
		m.From = fmt.Sprintf("%s <%s>", name, m.From)
	}
	if on("smtp_enabled") && str("smtp_host") != "" {
		port, _ := strconv.Atoi(fmt.Sprint(r["smtp_port"]))
		sec := strings.ToLower(str("smtp_encryption"))
		switch sec {
		case "ssl", "tls":
			sec = "tls"
		case "", "starttls":
			sec = "starttls"
		default:
			sec = "none"
		}
		m.SMTP = &SMTPConfig{Host: str("smtp_host"), Port: port, User: str("smtp_username"), Password: str("smtp_password"), From: m.From, Security: sec}
	}
	if on("resend_enabled") {
		m.Resend = str("resend_api_key")
	}
	if m.SMTP == nil && m.Resend == "" {
		return nil, errors.New("Coolify has no email set up (Settings → Email): set it there, or give coolify-mirror its own SMTP server")
	}
	return m, nil
}

func sendEmail(ctx context.Context, in *coolify.Instance, n NotifyConfig, subject, body string) error {
	cfg := n.SMTP
	if cfg == nil {
		m, err := readCoolifyMail(ctx, in)
		if err != nil {
			return err
		}
		if m.SMTP == nil {
			return sendResend(ctx, m.Resend, m.From, n.Email, subject, body)
		}
		cfg = m.SMTP
	}
	return sendSMTP(*cfg, n.Email, subject, body)
}

func sendSMTP(c SMTPConfig, to, subject, body string) error {
	if c.Port == 0 {
		c.Port = 587
	}
	if c.From == "" {
		c.From = c.User
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	var cl *smtp.Client
	var err error
	if c.Security == "tls" {
		conn, derr := tls.DialWithDialer(&net.Dialer{Timeout: 20 * time.Second}, "tcp", addr, &tls.Config{ServerName: c.Host})
		if derr != nil {
			return derr
		}
		cl, err = smtp.NewClient(conn, c.Host)
	} else {
		conn, derr := net.DialTimeout("tcp", addr, 20*time.Second)
		if derr != nil {
			return derr
		}
		cl, err = smtp.NewClient(conn, c.Host)
		if err == nil && c.Security != "none" {
			if ok, _ := cl.Extension("STARTTLS"); ok {
				err = cl.StartTLS(&tls.Config{ServerName: c.Host})
			} else if c.Security == "starttls" {
				err = errors.New("the mail server offers no STARTTLS")
			}
		}
	}
	if err != nil {
		return err
	}
	defer cl.Close()
	if c.User != "" {
		if err := cl.Auth(smtp.PlainAuth("", c.User, c.Password, c.Host)); err != nil {
			return err
		}
	}
	from := c.From
	if i := strings.LastIndex(from, "<"); i >= 0 {
		from = strings.Trim(from[i:], "<> ")
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	msg := "From: " + c.From + "\r\nTo: " + to + "\r\nSubject: " + subject +
		"\r\nDate: " + time.Now().Format(time.RFC1123Z) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(body, "\n", "\r\n") + "\r\n"
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// ResendAPI is the Resend endpoint (tests point it elsewhere).
var ResendAPI = "https://api.resend.com/emails"

func sendResend(ctx context.Context, key, from, to, subject, body string) error {
	b, _ := json.Marshal(map[string]any{"from": from, "to": []string{to}, "subject": subject, "text": body})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, ResendAPI, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Resend answered HTTP %d", resp.StatusCode)
	}
	return nil
}

// TelegramAPI is the Telegram Bot API base (tests point it elsewhere).
var TelegramAPI = "https://api.telegram.org"

func init() {
	if v := os.Getenv("COOLIFY_MIRROR_TELEGRAM_API"); v != "" {
		TelegramAPI = strings.TrimRight(v, "/")
	}
}

func readCoolifyTelegram(ctx context.Context, in *coolify.Instance) (token, chat string, err error) {
	if in == nil {
		return "", "", errors.New("no Coolify here")
	}
	var rows []map[string]any
	if err := in.Query(ctx, `SELECT * FROM telegram_notification_settings WHERE team_id = 0 LIMIT 1`, &rows); err != nil || len(rows) == 0 {
		return "", "", errors.New("Coolify has no Telegram bot set up (Notifications → Telegram): set it there, or give the bot token here")
	}
	t, _ := rows[0]["telegram_token"].(string)
	c, _ := rows[0]["telegram_chat_id"].(string)
	token, chat = strings.TrimSpace(plainValue(in, t)), strings.TrimSpace(plainValue(in, c))
	if token == "" {
		return "", "", errors.New("Coolify's Telegram settings have no bot token")
	}
	return token, chat, nil
}

func sendTelegram(ctx context.Context, in *coolify.Instance, n NotifyConfig, text string) error {
	token := n.TelegramToken
	if token == "" {
		t, _, err := readCoolifyTelegram(ctx, in)
		if err != nil {
			return err
		}
		token = t
	}
	form := url.Values{"chat_id": {n.TelegramChat}, "text": {text}, "disable_web_page_preview": {"true"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, TelegramAPI+"/bot"+token+"/sendMessage", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		// The URL holds the bot token: never show it.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("cannot reach Telegram (%v) - it is blocked in some countries; use email instead", err)
	}
	defer resp.Body.Close()
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if !out.OK {
		if out.Description == "" {
			out.Description = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("Telegram: %s (the person must send /start to the bot first; the chat id of a person is a number)", out.Description)
	}
	return nil
}

// RecoveryKeyMessage is the text that delivers a recovery key.
func RecoveryKeyMessage(host, secret string) (subject, body string) {
	subject = "coolify-mirror recovery key - " + host
	body = "This is the recovery key of the coolify-mirror backups of " + host + ".\n" +
		"Anyone with it can open those backups: keep this message safe (a password manager is best) and delete it from places you do not trust.\n\n" +
		secret + "\n\n" +
		"Restore on any server (same Coolify version, coolify-mirror installed, rclone set up if the backups are on a remote):\n" +
		"  sudo coolify-mirror restore REMOTE:folder --recovery-key " + secret + "\n" +
		"or run sudo coolify-mirror, choose Restore a backup and paste the key when asked.\n"
	return subject, body
}

// plainValue decrypts a Laravel-encrypted column value (or returns it as is).
func plainValue(in *coolify.Instance, v string) string {
	if v == "" || in == nil || in.Crypt == nil {
		return v
	}
	b, err := in.Crypt.DecryptString(v)
	if err != nil {
		return v
	}
	return dbx.DecodePlain(b)
}
