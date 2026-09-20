package discord

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/bwmarrin/discordgo"
	"nyan-go/internal/app"
)

var _ HandlerRegistrar = (*discordgo.Session)(nil)
var _ HandlerSender = (*discordgo.Session)(nil)
var _ app.MessageSender = (*DiscordSender)(nil)

const testGuild, testChannel, testUser = "guild", "channel", "user"

type fakeGatewaySession struct {
	handlers []interface{}
	removed  int
}

func (s *fakeGatewaySession) AddHandler(handler interface{}) func() {
	s.handlers = append(s.handlers, handler)
	return func() { s.removed++ }
}

type fakeSenderSession struct {
	messageErr       error
	complexErr       error
	fileErr          error
	interactionErr   error
	messageCalls     int
	complexCalls     int
	fileCalls        int
	interactionCalls int
	lastMessage      string
	lastFile         []byte
	lastComplex      *discordgo.MessageSend
}

func (s *fakeSenderSession) ChannelMessageSend(_ string, content string, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	s.messageCalls++
	s.lastMessage = content
	return nil, s.messageErr
}

func (s *fakeSenderSession) ChannelMessageSendComplex(_ string, data *discordgo.MessageSend, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	s.complexCalls++
	s.lastComplex = data
	return nil, s.complexErr
}

func (s *fakeSenderSession) ChannelFileSendWithMessage(_, _, _ string, reader io.Reader, _ ...discordgo.RequestOption) (*discordgo.Message, error) {
	s.fileCalls++
	s.lastFile, _ = io.ReadAll(reader)
	return nil, s.fileErr
}

func (s *fakeSenderSession) InteractionRespond(_ *discordgo.Interaction, _ *discordgo.InteractionResponse, _ ...discordgo.RequestOption) error {
	s.interactionCalls++
	return s.interactionErr
}

func testConfig() Config { return Config{GuildID: testGuild, ChannelID: testChannel, UserID: testUser} }

func TestGatewayAuthorizesMessageWithoutPrefixAndRejectsBoundaryViolations(t *testing.T) {
	var received []Message
	session := &fakeGatewaySession{}
	gateway, err := NewGateway(session, testConfig(), func(_ context.Context, message Message) error {
		received = append(received, message)
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	remove := gateway.RegisterHandlers()
	if len(session.handlers) != 2 {
		t.Fatalf("registered handlers = %d, want 2", len(session.handlers))
	}
	defer remove()

	handler := session.handlers[0].(func(*discordgo.Session, *discordgo.MessageCreate))
	for _, message := range []*discordgo.Message{
		{ID: "dm", ChannelID: testChannel, Author: &discordgo.User{ID: testUser}, Content: "hello"},
		{ID: "guild", GuildID: "other", ChannelID: testChannel, Author: &discordgo.User{ID: testUser}, Content: "hello"},
		{ID: "channel", GuildID: testGuild, ChannelID: "other", Author: &discordgo.User{ID: testUser}, Content: "hello"},
		{ID: "user", GuildID: testGuild, ChannelID: testChannel, Author: &discordgo.User{ID: "other"}, Content: "hello"},
		{ID: "bot", GuildID: testGuild, ChannelID: testChannel, Author: &discordgo.User{ID: testUser, Bot: true}, Content: "hello"},
		{ID: "nil-author", GuildID: testGuild, ChannelID: testChannel, Content: "hello"},
	} {
		handler(nil, &discordgo.MessageCreate{Message: message})
	}
	handler(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		ID: "accepted", GuildID: testGuild, ChannelID: testChannel,
		Author: &discordgo.User{ID: testUser}, Content: "trưa nay ăn phở 45k",
	}})

	if len(received) != 1 || received[0].ID != "accepted" || received[0].Content != "trưa nay ăn phở 45k" {
		t.Fatalf("received = %#v, want one unchanged authorized message", received)
	}
	remove()
	if session.removed != 2 {
		t.Fatalf("removed handlers = %d, want 2", session.removed)
	}
}

func TestGatewayAuthorizesButtonInteraction(t *testing.T) {
	var received []Interaction
	session := &fakeGatewaySession{}
	gateway, err := NewGateway(session, testConfig(), nil, func(_ context.Context, interaction Interaction) error {
		received = append(received, interaction)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway.RegisterHandlers()
	handler := session.handlers[1].(func(*discordgo.Session, *discordgo.InteractionCreate))
	button := func(guild, channel, user, customID string, componentType discordgo.ComponentType) *discordgo.InteractionCreate {
		return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
			ID: "interaction", Type: discordgo.InteractionMessageComponent,
			GuildID: guild, ChannelID: channel, Member: &discordgo.Member{User: &discordgo.User{ID: user}},
			Data: discordgo.MessageComponentInteractionData{ComponentType: componentType, CustomID: customID},
		}}
	}
	for _, event := range []*discordgo.InteractionCreate{
		button("other", testChannel, testUser, "confirm", discordgo.ButtonComponent),
		button(testGuild, "other", testUser, "confirm", discordgo.ButtonComponent),
		button(testGuild, testChannel, "other", "confirm", discordgo.ButtonComponent),
		button(testGuild, testChannel, testUser, "confirm", discordgo.SelectMenuComponent),
		button(testGuild, testChannel, testUser, "", discordgo.ButtonComponent),
	} {
		handler(nil, event)
	}
	handler(nil, button(testGuild, testChannel, testUser, "confirm:abc", discordgo.ButtonComponent))
	if len(received) != 1 || received[0].CustomID != "confirm:abc" {
		t.Fatalf("received = %#v, want one authorized button", received)
	}
}

func TestSenderRestrictsPrivateChannelAndPropagatesFailures(t *testing.T) {
	backend := &fakeSenderSession{messageErr: errors.New("send failed")}
	sender, err := NewSender(backend, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := sender.SendMessage(context.Background(), "other", "secret"); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("unauthorized channel error = %v, want ErrInvalidTarget", err)
	}
	if backend.messageCalls != 0 {
		t.Fatal("unauthorized send reached Discord backend")
	}
	if err := sender.SendMessage(context.Background(), testChannel, "secret"); !errors.Is(err, backend.messageErr) {
		t.Fatalf("send error = %v, want backend error", err)
	}
	if backend.messageCalls != 1 || backend.lastMessage != "secret" {
		t.Fatalf("message call = %d/%q", backend.messageCalls, backend.lastMessage)
	}
	if err := sender.SendFile(context.Background(), testChannel, "csv", "ledger.csv", bytes.NewBufferString("1,2\n")); err != nil {
		t.Fatalf("SendFile() error = %v", err)
	}
	if string(backend.lastFile) != "1,2\n" {
		t.Fatalf("file = %q", backend.lastFile)
	}
	if err := sender.SendButton(context.Background(), testChannel, "Xóa?", "Xác nhận", "confirm:1"); err != nil {
		t.Fatalf("SendButton() error = %v", err)
	}
	if backend.lastComplex == nil || len(backend.lastComplex.Components) != 1 {
		t.Fatal("button payload missing action row")
	}
	row, ok := backend.lastComplex.Components[0].(discordgo.ActionsRow)
	if !ok || len(row.Components) != 1 {
		t.Fatal("button payload missing button")
	}
	button, ok := row.Components[0].(discordgo.Button)
	if !ok || button.CustomID != "confirm:1" || button.Style != discordgo.DangerButton {
		t.Fatalf("button = %#v", row.Components[0])
	}
}

func TestSenderValidatesButtonInteractionAndContext(t *testing.T) {
	backend := &fakeSenderSession{interactionErr: errors.New("ack failed")}
	sender, err := NewSender(backend, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	interaction := &discordgo.Interaction{
		ID: "interaction", Type: discordgo.InteractionMessageComponent,
		GuildID: testGuild, ChannelID: testChannel, Member: &discordgo.Member{User: &discordgo.User{ID: testUser}},
		Data: discordgo.MessageComponentInteractionData{ComponentType: discordgo.ButtonComponent, CustomID: "confirm:1"},
	}
	if err := sender.RespondToButton(context.Background(), interaction, "done"); !errors.Is(err, backend.interactionErr) {
		t.Fatalf("interaction error = %v, want backend error", err)
	}
	if backend.interactionCalls != 1 {
		t.Fatalf("interaction calls = %d, want 1", backend.interactionCalls)
	}
	interaction.ChannelID = "other"
	if err := sender.RespondToButton(context.Background(), interaction, "done"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized interaction error = %v", err)
	}
	if backend.interactionCalls != 1 {
		t.Fatal("unauthorized interaction reached Discord backend")
	}
}
