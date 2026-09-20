package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
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
		log.Fatal("startup failed: ", err)
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
		started := time.Now()
		result, handleErr := orchestrator.Handle(ctx, ledger.RequestContext{UserID: message.UserID, GuildID: message.GuildID, ChannelID: message.ChannelID, SourceMessageID: message.ID, ReceivedAt: time.Now().In(cfg.Location)}, message.Content)
		if handleErr != nil {
			log.Printf("action=message success=false source=%s latency=%s error=%v", message.ID, time.Since(started), handleErr)
			return sender.SendMessage(ctx, message.ChannelID, "Mình chưa xử lý được yêu cầu này.")
		}
		log.Printf("action=message success=true source=%s latency=%s", message.ID, time.Since(started))
		if result == "" {
			return nil
		}
		if marker := strings.Index(result, "delete:"); marker >= 0 {
			token := strings.TrimSpace(result[marker+len("delete:"):])
			return sender.SendButton(ctx, message.ChannelID, "Xác nhận xóa giao dịch?", "Xóa", "delete:"+token)
		}
		if export := orchestrator.TakeExport(); len(export) > 0 {
			file, fileErr := os.CreateTemp("", "nyan-go-export-*.csv")
			if fileErr != nil {
				return fileErr
			}
			name := file.Name()
			defer os.Remove(name)
			if _, fileErr = file.Write(export); fileErr != nil {
				file.Close()
				return fileErr
			}
			if fileErr = file.Close(); fileErr != nil {
				return fileErr
			}
			read, fileErr := os.Open(name)
			if fileErr != nil {
				return fileErr
			}
			defer read.Close()
			return sender.SendFile(ctx, message.ChannelID, result, "transactions.csv", read)
		}
		if strings.HasPrefix(result, "id,type,amount_vnd,") {
			file, fileErr := os.CreateTemp("", "nyan-go-export-*.csv")
			if fileErr != nil {
				return fileErr
			}
			name := file.Name()
			defer os.Remove(name)
			if _, fileErr = file.WriteString(result); fileErr != nil {
				file.Close()
				return fileErr
			}
			if fileErr = file.Close(); fileErr != nil {
				return fileErr
			}
			read, fileErr := os.Open(name)
			if fileErr != nil {
				return fileErr
			}
			defer read.Close()
			return sender.SendFile(ctx, message.ChannelID, "", "transactions.csv", read)
		}
		return sender.SendMessage(ctx, message.ChannelID, result)
	}
	interactionHandler := func(ctx context.Context, interaction discord.Interaction) error {
		if !strings.HasPrefix(interaction.CustomID, "delete:") || interaction.Raw == nil {
			return nil
		}
		token := strings.TrimPrefix(interaction.CustomID, "delete:")
		_, confirmErr := orchestrator.ConfirmDelete(ctx, ledger.RequestContext{UserID: interaction.UserID, GuildID: interaction.GuildID, ChannelID: interaction.ChannelID, SourceMessageID: interaction.ID, ReceivedAt: time.Now().In(cfg.Location)}, token)
		content := "Giao dịch đã được xóa."
		if confirmErr != nil {
			content = "Không thể xác nhận thao tác xóa."
		}
		return sender.RespondToButton(ctx, interaction.Raw, content)
	}
	gateway, err := discord.NewGateway(session, discord.Config{GuildID: cfg.DiscordGuildID, ChannelID: cfg.DiscordChannelID, UserID: cfg.DiscordUserID}, messageHandler, interactionHandler)
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
