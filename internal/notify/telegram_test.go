package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSendDisabledNoops(t *testing.T) {
	c := TelegramConfig{}
	if c.Enabled() {
		t.Fatal("empty config should be disabled")
	}
	if err := c.Send("hello"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
}

func TestSendPostsMessage(t *testing.T) {
	var gotBody string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := TelegramConfig{Token: "TESTTOKEN", ChatID: "42", BaseURL: srv.URL}
	if err := c.Send("backup ok"); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if !strings.Contains(gotPath, "botTESTTOKEN/sendMessage") {
		t.Fatalf("path = %q", gotPath)
	}
	vals, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if vals.Get("chat_id") != "42" || vals.Get("text") != "backup ok" {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestSendHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"description":"bad request"}`))
	}))
	defer srv.Close()

	c := TelegramConfig{Token: "T", ChatID: "1", BaseURL: srv.URL}
	if err := c.Send("x"); err == nil {
		t.Fatal("Send() succeeded on HTTP error")
	}
}

func TestSendRedactsTokenFromTransportError(t *testing.T) {
	// Nothing listens on this port, so the transport error embeds the
	// request URL, which contains the bot token.
	c := TelegramConfig{
		Token:   "SECRETTOKEN123",
		ChatID:  "1",
		BaseURL: "http://127.0.0.1:1",
	}
	err := c.Send("x")
	if err == nil {
		t.Fatal("Send() succeeded against an unreachable server")
	}
	if strings.Contains(err.Error(), "SECRETTOKEN123") {
		t.Fatalf("error leaks the bot token: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("error = %v, want the token placeholder", err)
	}
}
