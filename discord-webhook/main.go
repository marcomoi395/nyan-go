package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	password := os.Getenv("DISCORD_WEB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		return errors.New("missing required configuration DISCORD_WEB_PASSWORD")
	}
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if !validWebhookURL(webhookURL) {
		return errors.New("DISCORD_WEBHOOK_URL must be an HTTPS Discord webhook URL")
	}
	server := &http.Server{
		Addr: ":8080", Handler: newHandler(password, webhookURL, &http.Client{Timeout: 30 * time.Second}),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second,
		WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("web server failed: %w", err)
	}
	return nil
}

func validWebhookURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "discord.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	parts := strings.Split(u.Path, "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "api" || parts[2] != "webhooks" || parts[3] == "" || parts[4] == "" {
		return false
	}
	for _, digit := range parts[3] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	for _, character := range parts[4] {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.') {
			return false
		}
	}
	return true
}
