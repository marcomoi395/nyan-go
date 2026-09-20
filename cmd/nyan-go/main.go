package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"nyan-go/internal/app"
	"nyan-go/internal/config"
	"nyan-go/internal/discord"
	"nyan-go/internal/ledger"
	"nyan-go/internal/provider/responses"
	"nyan-go/internal/reminder"
	"nyan-go/internal/storage/sqlite"
)

func main() {
	if err := run(); err != nil {
		log.Printf("startup failed: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := sqlite.Open(cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer store.Close()
	provider, err := responses.NewFromConfig(cfg)
	if err != nil {
		return err
	}
	service, err := app.NewLedgerService(store, cfg.Location)
	if err != nil {
		return err
	}
	orchestrator, err := app.NewOrchestrator(provider, service, nil)
	if err != nil {
		return err
	}
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return err
	}
	sender, err := discord.NewSender(session, discord.Config{GuildID: cfg.DiscordGuildID, ChannelID: cfg.DiscordChannelID, UserID: cfg.DiscordUserID})
	if err != nil {
		return err
	}
	messageHandler := func(ctx context.Context, message discord.Message) error {
		result, handleErr := orchestrator.Handle(ctx, ledger.RequestContext{UserID: message.UserID, GuildID: message.GuildID, ChannelID: message.ChannelID, SourceMessageID: message.ID, ReceivedAt: time.Now().In(cfg.Location)}, message.Content)
		if handleErr != nil {
			log.Printf("message failed source=%s: %v", message.ID, handleErr)
			return sender.SendMessage(ctx, message.ChannelID, "Mình chưa xử lý được yêu cầu này.")
		}
		if result == "" {
			return nil
		}
		return sender.SendMessage(ctx, message.ChannelID, result)
	}
	gateway, err := discord.NewGateway(session, discord.Config{GuildID: cfg.DiscordGuildID, ChannelID: cfg.DiscordChannelID, UserID: cfg.DiscordUserID}, messageHandler, nil)
	if err != nil {
		return err
	}
	cleanup := gateway.RegisterHandlers()
	defer cleanup()
	if err := session.Open(); err != nil {
		return err
	}
	defer session.Close()
	if cfg.ReminderEnabled {
		scheduler, scheduleErr := reminder.NewScheduler(store, sender, reminder.Config{UserID: cfg.DiscordUserID, GuildID: cfg.DiscordGuildID, ChannelID: cfg.DiscordChannelID, ReminderTime: cfg.ReminderTime, ReminderCooldown: cfg.ReminderCooldown, Location: cfg.Location}, systemClock{})
		if scheduleErr != nil {
			return scheduleErr
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		go func() { _ = scheduler.Run(ctx) }()
		<-ctx.Done()
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
