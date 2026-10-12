package notify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TelegramConfig holds Bot API credentials. Empty token disables notify.
type TelegramConfig struct {
	Token  string
	ChatID string
	// BaseURL overrides the Bot API root for tests.
	BaseURL string
}

// FromEnv reads GLOAK_TELEGRAM_TOKEN and GLOAK_TELEGRAM_CHAT_ID.
func FromEnv() TelegramConfig {
	return TelegramConfig{
		Token:  os.Getenv("GLOAK_TELEGRAM_TOKEN"),
		ChatID: os.Getenv("GLOAK_TELEGRAM_CHAT_ID"),
	}
}

// Enabled reports whether notify should attempt a send.
func (c TelegramConfig) Enabled() bool {
	return c.Token != "" && c.ChatID != ""
}

// Send posts message to Telegram, truncating it to 4000 bytes with an ellipsis.
// It does nothing when credentials are incomplete. It uses a 15-second HTTP
// timeout and returns request errors or errors for non-200 responses.
func (c TelegramConfig) Send(message string) error {
	if !c.Enabled() {
		return nil
	}
	base := c.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", base, c.Token)

	form := url.Values{}
	form.Set("chat_id", c.ChatID)
	form.Set("text", truncate(message, 4000))

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Post(endpoint, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var body struct {
			Description string `json:"description"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return fmt.Errorf("telegram send: status %d: %s", resp.StatusCode, body.Description)
	}
	return nil
}

// truncate limits s to n bytes, ending shortened text with "...". It may split
// a UTF-8 sequence and requires n >= 3 when truncation is needed.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
