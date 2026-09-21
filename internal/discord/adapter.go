// Package discord contains the Discord gateway boundary. It is the only
// package that needs to know about discordgo event and message types.
package discord

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	ErrUnauthorized       = errors.New("discord event is not authorized")
	ErrInvalidTarget      = errors.New("discord target is not the configured private channel")
	ErrInvalidInteraction = errors.New("discord interaction is not an authorized button interaction")
)

// Config identifies the one guild, private channel, and user served by the
// bot. BotUserID is optional; discordgo's Author.Bot flag still rejects bots.
type Config struct {
	GuildID   string
	ChannelID string
	UserID    string
	BotUserID string
}

func (c Config) Validate() error {
	for name, value := range map[string]string{
		"guild ID": c.GuildID, "channel ID": c.ChannelID, "user ID": c.UserID,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

// Message is the application-facing representation of an authorized Discord
// message. Its identity fields come from the gateway event, not user text.
type Message struct {
	ID        string
	GuildID   string
	ChannelID string
	UserID    string
	Content   string
}

// Interaction is the application-facing representation of an authorized
// button interaction.
type Interaction struct {
	ID        string
	GuildID   string
	ChannelID string
	UserID    string
	CustomID  string
	Raw       *discordgo.Interaction
}

type MessageHandler func(context.Context, Message) error
type InteractionHandler func(context.Context, Interaction) error

// HandlerRegistrar is implemented by *discordgo.Session and is small enough
// to replace in tests without opening a Discord connection.
type HandlerRegistrar interface {
	AddHandler(interface{}) func()
}

// Gateway wires discordgo events to authorized application callbacks.
type Gateway struct {
	session            HandlerRegistrar
	config             Config
	messageHandler     MessageHandler
	interactionHandler InteractionHandler
}

func NewGateway(session HandlerRegistrar, config Config, messageHandler MessageHandler, interactionHandler InteractionHandler) (*Gateway, error) {
	if session == nil {
		return nil, errors.New("discord session is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Gateway{session: session, config: config, messageHandler: messageHandler, interactionHandler: interactionHandler}, nil
}

// RegisterHandlers installs message and component handlers and returns an
// idempotent-enough cleanup function for callers shutting down the bot.
func (g *Gateway) RegisterHandlers() func() {
	removeMessage := g.session.AddHandler(g.handleMessageCreate)
	removeInteraction := g.session.AddHandler(g.handleInteractionCreate)
	return func() {
		if removeMessage != nil {
			removeMessage()
		}
		if removeInteraction != nil {
			removeInteraction()
		}
	}
}

func (g *Gateway) handleMessageCreate(_ *discordgo.Session, event *discordgo.MessageCreate) {
	if event == nil || event.Message == nil || !g.acceptMessage(event.Message) || g.messageHandler == nil {
		return
	}
	message := Message{
		ID: event.ID, GuildID: event.GuildID, ChannelID: event.ChannelID,
		UserID: event.Author.ID, Content: event.Content,
	}
	_ = g.messageHandler(context.Background(), message)
}

func (g *Gateway) handleInteractionCreate(_ *discordgo.Session, event *discordgo.InteractionCreate) {
	if event == nil || event.Interaction == nil || !g.acceptInteraction(event.Interaction) || g.interactionHandler == nil {
		return
	}
	data, ok := componentData(event.Data)
	if !ok {
		return
	}
	userID := interactionUserID(event.Interaction)
	_ = g.interactionHandler(context.Background(), Interaction{
		ID: event.ID, GuildID: event.GuildID, ChannelID: event.ChannelID,
		UserID: userID, CustomID: data.CustomID, Raw: event.Interaction,
	})
}

func (g *Gateway) acceptMessage(message *discordgo.Message) bool {
	if message == nil || message.Author == nil || message.Author.Bot {
		return false
	}
	if g.config.BotUserID != "" && message.Author.ID == g.config.BotUserID {
		return false
	}
	// Discord DMs have no guild ID. Requiring all three IDs also prevents
	// malformed events from reaching the application callback.
	return message.GuildID == g.config.GuildID && message.ChannelID == g.config.ChannelID && message.Author.ID == g.config.UserID
}

func (g *Gateway) acceptInteraction(interaction *discordgo.Interaction) bool {
	if interaction == nil || interaction.Type != discordgo.InteractionMessageComponent {
		return false
	}
	if interaction.GuildID != g.config.GuildID || interaction.ChannelID != g.config.ChannelID || interactionUserID(interaction) != g.config.UserID {
		return false
	}
	data, ok := componentData(interaction.Data)
	return ok && data.ComponentType == discordgo.ButtonComponent && data.CustomID != ""
}

func componentData(value discordgo.InteractionData) (discordgo.MessageComponentInteractionData, bool) {
	switch data := value.(type) {
	case discordgo.MessageComponentInteractionData:
		return data, true
	case *discordgo.MessageComponentInteractionData:
		if data != nil {
			return *data, true
		}
	}
	return discordgo.MessageComponentInteractionData{}, false
}

func interactionUserID(interaction *discordgo.Interaction) string {
	if interaction == nil {
		return ""
	}
	if interaction.Member != nil && interaction.Member.User != nil {
		return interaction.Member.User.ID
	}
	if interaction.User != nil {
		return interaction.User.ID
	}
	return ""
}

// DiscordSender implements app.MessageSender while enforcing the configured
// private channel at the boundary.
type DiscordSender struct {
	session HandlerSender
	config  Config
}

// HandlerSender is the send subset of discordgo.Session. Keeping this
// interface narrow makes send and failure paths testable without Discord.
type HandlerSender interface {
	ChannelMessageSend(string, string, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageSendComplex(string, *discordgo.MessageSend, ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelFileSendWithMessage(string, string, string, io.Reader, ...discordgo.RequestOption) (*discordgo.Message, error)
	InteractionRespond(*discordgo.Interaction, *discordgo.InteractionResponse, ...discordgo.RequestOption) error
}

func NewSender(session HandlerSender, config Config) (*DiscordSender, error) {
	if session == nil {
		return nil, errors.New("discord session is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &DiscordSender{session: session, config: config}, nil
}

func (s *DiscordSender) SendMessage(ctx context.Context, channelID, content string) error {
	if err := s.beforeSend(ctx, channelID); err != nil {
		return err
	}
	_, err := s.session.ChannelMessageSend(channelID, content)
	return err
}

func (s *DiscordSender) SendFile(ctx context.Context, channelID, content, filename string, reader io.Reader) error {
	if err := s.beforeSend(ctx, channelID); err != nil {
		return err
	}
	if reader == nil {
		return errors.New("file reader is required")
	}
	_, err := s.session.ChannelFileSendWithMessage(channelID, content, filename, reader)
	return err
}

func (s *DiscordSender) SendButton(ctx context.Context, channelID, content, label, customID string) error {
	if err := s.beforeSend(ctx, channelID); err != nil {
		return err
	}
	if strings.TrimSpace(label) == "" || strings.TrimSpace(customID) == "" {
		return errors.New("button label and custom ID are required")
	}
	_, err := s.session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		Components: []discordgo.MessageComponent{discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{discordgo.Button{
				Label: label, Style: discordgo.DangerButton, CustomID: customID,
			}},
		}},
	})
	return err
}

// RespondToButton acknowledges an authorized button interaction. It validates
// the interaction source before touching Discord's interaction endpoint.
func (s *DiscordSender) RespondToButton(ctx context.Context, interaction *discordgo.Interaction, content string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if interaction == nil || interaction.GuildID != s.config.GuildID || interaction.ChannelID != s.config.ChannelID || interactionUserID(interaction) != s.config.UserID {
		return ErrUnauthorized
	}
	data, ok := componentData(interaction.Data)
	if !ok || data.ComponentType != discordgo.ButtonComponent || data.CustomID == "" {
		return ErrInvalidInteraction
	}
	return s.session.InteractionRespond(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}

func (s *DiscordSender) beforeSend(ctx context.Context, channelID string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if channelID != s.config.ChannelID {
		return ErrInvalidTarget
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	return ctx.Err()
}
