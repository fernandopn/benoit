//go:build ignore

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fernandopn/benoit/channels"
	"golang.org/x/term"
)

const (
	telegramPollTimeoutSeconds = 30
	typingEventInterval        = 5 * time.Second
)

var (
	errUnexpectedPositionalArgs = errors.New("unexpected positional arguments")
	errTelegramTokenRequired    = errors.New("-telegram-token is required")
	errInteractiveRequiresTTY   = errors.New("interactive mode requires a terminal")
	errIncomingChannelClosed    = errors.New("incoming message channel closed")
)

type inputEventType int

const (
	inputEventRune inputEventType = iota
	inputEventEnter
	inputEventTab
	inputEventBackspace
	inputEventQuit
	inputEventEOF
	inputEventError
)

type inputEvent struct {
	typeID inputEventType
	r      rune
	err    error
}

type chatState struct {
	recipients          []int64
	recipientSet        map[int64]struct{}
	recipientUsernames  map[int64]string
	recipientDisplayMap map[int64]string
	recipientIndex      int
	input               []rune
}

type typingController struct {
	interval        time.Duration
	activeRecipient int64
	lastSent        map[int64]time.Time
}

type interactiveSession struct {
	ctx       context.Context
	channel   channels.Channel
	incoming  <-chan channels.ChannelMessage
	listenErr <-chan error

	inputEvents <-chan inputEvent
	writer      *bufio.Writer
	colors      simpleTheme
	width       int
	verbose     bool

	chat   *chatState
	typing *typingController
}

func main() {
	if err := runChannelTUI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runChannelTUI(args []string) error {
	flagSet := flag.NewFlagSet("channels-tui", flag.ContinueOnError)
	telegramToken := flagSet.String("telegram-token", "", "telegram bot token")
	verbose := flagSet.Bool("v", false, "verbose debug output")
	if err := flagSet.Parse(args); err != nil {
		return err
	}
	if len(flagSet.Args()) > 0 {
		return errUnexpectedPositionalArgs
	}

	token := strings.TrimSpace(*telegramToken)
	if token == "" {
		return errTelegramTokenRequired
	}

	telegramClient, err := channels.NewTelegram(token, http.DefaultClient)
	if err != nil {
		return err
	}
	var channel channels.Channel = telegramClient

	incoming := make(chan channels.ChannelMessage, 64)
	if err := channel.RegisterReceiveMessageChan(incoming); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	listenErr := make(chan error, 1)
	go func() {
		listenErr <- channel.Listen(ctx, telegramPollTimeoutSeconds)
	}()

	return runInteractiveChat(ctx, channel, incoming, listenErr, *verbose)
}

func runInteractiveChat(ctx context.Context, channel channels.Channel, incoming <-chan channels.ChannelMessage, listenErr <-chan error, verbose bool) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errInteractiveRequiresTTY
	}

	colors := newSimpleTheme(term.IsTerminal(int(os.Stdout.Fd())))
	width := terminalWidth()

	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer term.Restore(fd, state)

	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	reader := bufio.NewReader(os.Stdin)
	inputEvents := make(chan inputEvent, 16)
	go readInputEvents(reader, inputEvents)

	session := &interactiveSession{
		ctx:         ctx,
		channel:     channel,
		incoming:    incoming,
		listenErr:   listenErr,
		inputEvents: inputEvents,
		writer:      writer,
		colors:      colors,
		width:       width,
		verbose:     verbose,
		chat:        newChatState(),
		typing:      newTypingController(typingEventInterval),
	}

	return session.run()
}

func (s *interactiveSession) run() error {
	defer s.stopTypingIfNeeded()

	writeChannelHeader(s.writer, s.colors, s.width)
	printLine(s.writer, s.colors, "Waiting for incoming messages to discover recipients...", s.colors.dim, s.colors.fgMuted)
	debugLine(s.writer, s.colors, s.verbose, "verbose mode enabled")
	printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
	renderPrompt(s.writer, s.colors, s.chat)

	for {
		select {
		case <-s.ctx.Done():
			debugLine(s.writer, s.colors, s.verbose, "context canceled")
			printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
			return nil
		case err := <-s.listenErr:
			if handled, stop := s.handleListenErr(err); stop {
				return handled
			}
		case message, ok := <-s.incoming:
			if err := s.handleIncomingMessage(message, ok); err != nil {
				return err
			}
		case event, ok := <-s.inputEvents:
			done, err := s.handleInputEvent(event, ok)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

func (s *interactiveSession) handleListenErr(err error) (error, bool) {
	debugLine(s.writer, s.colors, s.verbose, "listen returned err=%v", err)
	if err == nil || errors.Is(err, context.Canceled) {
		printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
		return nil, true
	}
	return err, true
}

func (s *interactiveSession) handleIncomingMessage(message channels.ChannelMessage, ok bool) error {
	if !ok {
		return errIncomingChannelClosed
	}

	debugLine(s.writer, s.colors, s.verbose, "incoming raw user_id=%d type=%d params=%v text=%q", message.UserID, message.Type, message.Params, strings.TrimSpace(message.Text))
	if message.Type != channels.TextMessage {
		debugLine(s.writer, s.colors, s.verbose, "ignoring non-text message type=%d", message.Type)
		return nil
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		debugLine(s.writer, s.colors, s.verbose, "ignoring empty text message user_id=%d", message.UserID)
		return nil
	}

	username := channelMessageUsername(message)
	displayName := channelMessageDisplayName(message)
	if username == "" {
		debugLine(s.writer, s.colors, s.verbose, "username not found in params for user_id=%d", message.UserID)
	} else {
		debugLine(s.writer, s.colors, s.verbose, "resolved username @%s for user_id=%d", username, message.UserID)
	}
	if displayName == "" {
		debugLine(s.writer, s.colors, s.verbose, "display_name not found in params for user_id=%d", message.UserID)
	} else {
		debugLine(s.writer, s.colors, s.verbose, "resolved display_name %q for user_id=%d", displayName, message.UserID)
	}

	s.chat.addRecipient(message.UserID, username, displayName)
	debugLine(s.writer, s.colors, s.verbose, "recipient list=%v current=%d", s.chat.recipients, s.chat.currentRecipient())
	printIncomingMessage(s.writer, s.colors, s.chat.recipientDisplay(message.UserID), text)
	renderPrompt(s.writer, s.colors, s.chat)

	return nil
}

func (s *interactiveSession) handleInputEvent(event inputEvent, ok bool) (bool, error) {
	if !ok {
		printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
		return true, nil
	}

	switch event.typeID {
	case inputEventError:
		if errors.Is(event.err, io.EOF) {
			printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
			return true, nil
		}
		return false, event.err
	case inputEventQuit, inputEventEOF:
		debugLine(s.writer, s.colors, s.verbose, "input requested exit")
		printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
		return true, nil
	case inputEventTab:
		s.handleTab()
		return false, nil
	case inputEventBackspace:
		s.chat.backspace()
		renderPrompt(s.writer, s.colors, s.chat)
		return false, nil
	case inputEventRune:
		s.chat.appendRune(event.r)
		s.sendTypingIfNeeded()
		renderPrompt(s.writer, s.colors, s.chat)
		return false, nil
	case inputEventEnter:
		return s.handleEnter()
	default:
		return false, nil
	}
}

func (s *interactiveSession) handleTab() {
	if !s.chat.cycleRecipient() {
		printLine(s.writer, s.colors, "No recipients yet", s.colors.fgWarn)
		debugLine(s.writer, s.colors, s.verbose, "tab pressed with no recipients")
	} else {
		debugLine(s.writer, s.colors, s.verbose, "tab switched to recipient=%d display=%q", s.chat.currentRecipient(), s.chat.recipientDisplay(s.chat.currentRecipient()))
		s.sendTypingIfNeeded()
	}

	renderPrompt(s.writer, s.colors, s.chat)
}

func (s *interactiveSession) handleEnter() (bool, error) {
	s.stopTypingIfNeeded()

	line := s.chat.takeInputLine()
	if line == "" {
		renderPrompt(s.writer, s.colors, s.chat)
		return false, nil
	}
	if line == "/exit" || line == "/quit" {
		printLine(s.writer, s.colors, "", s.colors.dim, s.colors.fgMuted)
		return true, nil
	}

	recipient := s.chat.currentRecipient()
	if recipient == 0 {
		printLine(s.writer, s.colors, "No recipient selected yet. Wait for an incoming message.", s.colors.fgWarn)
		debugLine(s.writer, s.colors, s.verbose, "send blocked: no current recipient")
		renderPrompt(s.writer, s.colors, s.chat)
		return false, nil
	}

	debugLine(s.writer, s.colors, s.verbose, "sending message to recipient=%d display=%q text=%q", recipient, s.chat.recipientDisplay(recipient), line)
	err := s.channel.SendMessage(s.ctx, channels.ChannelMessage{UserID: recipient, Type: channels.TextMessage, Text: line})
	if err != nil {
		printLine(s.writer, s.colors, "Send error: "+err.Error(), s.colors.bold, s.colors.fgWarn)
		debugLine(s.writer, s.colors, s.verbose, "send failed: %v", err)
		renderPrompt(s.writer, s.colors, s.chat)
		return false, nil
	}

	debugLine(s.writer, s.colors, s.verbose, "send succeeded")
	printOutgoingMessage(s.writer, s.colors, s.chat.recipientDisplay(recipient), line)
	renderPrompt(s.writer, s.colors, s.chat)

	return false, nil
}

func (s *interactiveSession) sendTypingIfNeeded() {
	if len(s.chat.input) == 0 {
		return
	}

	recipient := s.chat.currentRecipient()
	if recipient == 0 {
		return
	}

	sent, err := s.typing.notify(s.ctx, s.channel, recipient)
	if err != nil {
		debugLine(s.writer, s.colors, s.verbose, "typing send failed recipient=%d err=%v", recipient, err)
		return
	}
	if sent {
		debugLine(s.writer, s.colors, s.verbose, "typing sent recipient=%d", recipient)
		return
	}
	debugLine(s.writer, s.colors, s.verbose, "typing throttled recipient=%d", recipient)
}

func (s *interactiveSession) stopTypingIfNeeded() {
	recipient, err := s.typing.stop(s.ctx, s.channel)
	if recipient == 0 {
		return
	}
	if err != nil {
		debugLine(s.writer, s.colors, s.verbose, "stop typing failed recipient=%d err=%v", recipient, err)
		return
	}
	debugLine(s.writer, s.colors, s.verbose, "stop typing recipient=%d", recipient)
}

func newTypingController(interval time.Duration) *typingController {
	return &typingController{interval: interval, lastSent: make(map[int64]time.Time)}
}

func (t *typingController) notify(ctx context.Context, channel channels.Channel, recipient int64) (bool, error) {
	if recipient == 0 {
		return false, nil
	}

	now := time.Now()
	if t.interval > 0 {
		if lastSent, ok := t.lastSent[recipient]; ok && now.Sub(lastSent) < t.interval {
			return false, nil
		}
	}

	if err := sendTypingEvent(ctx, channel, recipient, true); err != nil {
		return false, err
	}

	t.activeRecipient = recipient
	t.lastSent[recipient] = now
	return true, nil
}

func (t *typingController) stop(ctx context.Context, channel channels.Channel) (int64, error) {
	recipient := t.activeRecipient
	if recipient == 0 {
		return 0, nil
	}
	t.activeRecipient = 0
	return recipient, sendTypingEvent(ctx, channel, recipient, false)
}

func debugLine(writer *bufio.Writer, colors simpleTheme, enabled bool, format string, args ...any) {
	if !enabled {
		return
	}
	printLine(writer, colors, "[debug] "+fmt.Sprintf(format, args...), colors.dim, colors.fgMuted)
}

func sendTypingEvent(ctx context.Context, channel channels.Channel, userID int64, typing bool) error {
	if userID == 0 {
		return nil
	}
	return channel.SendMessage(ctx, channels.ChannelMessage{UserID: userID, Type: channels.TypingEvent, Typing: typing})
}

func readInputEvents(reader *bufio.Reader, out chan<- inputEvent) {
	defer close(out)

	for {
		b, err := reader.ReadByte()
		if err != nil {
			out <- inputEvent{typeID: inputEventError, err: err}
			return
		}

		switch b {
		case '\r', '\n':
			out <- inputEvent{typeID: inputEventEnter}
		case '\t':
			out <- inputEvent{typeID: inputEventTab}
		case 0x03:
			out <- inputEvent{typeID: inputEventQuit}
		case 0x04:
			out <- inputEvent{typeID: inputEventEOF}
		case 0x08, 0x7f:
			out <- inputEvent{typeID: inputEventBackspace}
		case 0x1b:
			discardEscapeSequence(reader)
		default:
			if b < 0x20 {
				continue
			}
			r, err := decodeRuneFromFirstByte(reader, b)
			if err != nil {
				out <- inputEvent{typeID: inputEventError, err: err}
				return
			}
			out <- inputEvent{typeID: inputEventRune, r: r}
		}
	}
}

func discardEscapeSequence(reader *bufio.Reader) {
	for reader.Buffered() > 0 {
		b, err := reader.ReadByte()
		if err != nil {
			return
		}
		if b >= 0x40 && b <= 0x7e {
			return
		}
	}
}

func decodeRuneFromFirstByte(reader *bufio.Reader, first byte) (rune, error) {
	if first < utf8.RuneSelf {
		return rune(first), nil
	}

	need := utf8SequenceLength(first)
	if need == 1 {
		return rune(first), nil
	}

	buf := make([]byte, 0, need)
	buf = append(buf, first)
	for len(buf) < need {
		b, err := reader.ReadByte()
		if err != nil {
			return 0, err
		}
		buf = append(buf, b)
	}

	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size == 1 {
		return rune(first), nil
	}
	return r, nil
}

func utf8SequenceLength(first byte) int {
	switch {
	case first&0x80 == 0x00:
		return 1
	case first&0xe0 == 0xc0:
		return 2
	case first&0xf0 == 0xe0:
		return 3
	case first&0xf8 == 0xf0:
		return 4
	default:
		return 1
	}
}

func newChatState() *chatState {
	return &chatState{
		recipients:          make([]int64, 0, 8),
		recipientSet:        make(map[int64]struct{}),
		recipientUsernames:  make(map[int64]string),
		recipientDisplayMap: make(map[int64]string),
		recipientIndex:      -1,
		input:               make([]rune, 0, 128),
	}
}

func (s *chatState) addRecipient(userID int64, username string, displayName string) {
	if userID == 0 {
		return
	}

	if username != "" {
		s.recipientUsernames[userID] = normalizeUsername(username)
	}
	if displayName != "" {
		s.recipientDisplayMap[userID] = strings.TrimSpace(displayName)
	}

	if _, exists := s.recipientSet[userID]; !exists {
		s.recipientSet[userID] = struct{}{}
		s.recipients = append(s.recipients, userID)
		if s.recipientIndex < 0 {
			s.recipientIndex = 0
		}
	}
}

func (s *chatState) recipientDisplay(userID int64) string {
	if userID == 0 {
		return "none"
	}

	if username := normalizeUsername(s.recipientUsernames[userID]); username != "" {
		return "@" + username
	}

	displayName := strings.TrimSpace(s.recipientDisplayMap[userID])
	if displayName != "" {
		return displayName
	}

	return "unknown"
}

func (s *chatState) currentRecipientPromptLabel() string {
	userID := s.currentRecipient()
	if userID == 0 {
		return "none"
	}

	if username := normalizeUsername(s.recipientUsernames[userID]); username != "" {
		return "@" + username
	}

	displayName := strings.TrimSpace(s.recipientDisplayMap[userID])
	if displayName != "" {
		return displayName
	}

	return "unknown"
}

func channelMessageUsername(message channels.ChannelMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	return normalizeUsername(message.Params[channels.ParamUsername])
}

func channelMessageDisplayName(message channels.ChannelMessage) string {
	if len(message.Params) == 0 {
		return ""
	}
	return strings.TrimSpace(message.Params[channels.ParamDisplayName])
}

func normalizeUsername(username string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	return strings.TrimPrefix(username, "@")
}

func (s *chatState) currentRecipient() int64 {
	if s.recipientIndex < 0 || s.recipientIndex >= len(s.recipients) {
		return 0
	}
	return s.recipients[s.recipientIndex]
}

func (s *chatState) cycleRecipient() bool {
	if len(s.recipients) == 0 {
		return false
	}
	if s.recipientIndex < 0 {
		s.recipientIndex = 0
		return true
	}
	s.recipientIndex = (s.recipientIndex + 1) % len(s.recipients)
	return true
}

func (s *chatState) appendRune(r rune) {
	s.input = append(s.input, r)
}

func (s *chatState) backspace() {
	if len(s.input) == 0 {
		return
	}
	s.input = s.input[:len(s.input)-1]
}

func (s *chatState) takeInputLine() string {
	line := strings.TrimSpace(string(s.input))
	s.input = s.input[:0]
	return line
}

func renderPrompt(writer *bufio.Writer, colors simpleTheme, state *chatState) {
	clearLine(writer)

	recipient := state.currentRecipientPromptLabel()
	prefixColor := colors.fgWarn
	if state.currentRecipient() != 0 {
		prefixColor = colors.fgAccent
	}

	prefix := fmt.Sprintf("to[%s]> ", recipient)
	fmt.Fprint(writer, colors.style(prefix, colors.bold, prefixColor))
	fmt.Fprint(writer, colors.style(string(state.input), colors.fgStrong))
	writer.Flush()
}

func writeChannelHeader(writer *bufio.Writer, colors simpleTheme, width int) {
	left := "Benoit · Telegram Chat"
	fmt.Fprintln(writer, colors.style(left, colors.bold, colors.fgAccent))
	if width > 0 {
		hint := "Enter send | Tab switch recipient | /exit to quit"
		fmt.Fprintln(writer, colors.style(hint, colors.dim, colors.fgMuted))
	}
	fmt.Fprintln(writer)
	writer.Flush()
}

func printIncomingMessage(writer *bufio.Writer, colors simpleTheme, sender string, text string) {
	printLine(writer, colors, sender, colors.bold, colors.fgAccent)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		printLine(writer, colors, line, colors.fgUser)
	}
	printLine(writer, colors, "", colors.dim, colors.fgMuted)
}

func printOutgoingMessage(writer *bufio.Writer, colors simpleTheme, recipient string, text string) {
	printLine(writer, colors, "you -> "+recipient, colors.bold, colors.fgMuted)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		printLine(writer, colors, line, colors.fgStrong)
	}
	printLine(writer, colors, "", colors.dim, colors.fgMuted)
}

func printLine(writer *bufio.Writer, colors simpleTheme, line string, styles ...string) {
	clearLine(writer)
	if line == "" {
		fmt.Fprint(writer, "\r\n")
		writer.Flush()
		return
	}
	fmt.Fprint(writer, colors.style(line, styles...), "\r\n")
	writer.Flush()
}

func clearLine(writer *bufio.Writer) {
	fmt.Fprint(writer, "\r\x1b[2K")
}

type simpleTheme struct {
	enabled  bool
	reset    string
	bold     string
	dim      string
	fgStrong string
	fgMuted  string
	fgAccent string
	fgWarn   string
	fgUser   string
	bgUser   string
}

func newSimpleTheme(enabled bool) simpleTheme {
	return simpleTheme{
		enabled:  enabled,
		reset:    "\x1b[0m",
		bold:     "\x1b[1m",
		dim:      "\x1b[2m",
		fgStrong: ansiForeground("FFFFFF"),
		fgMuted:  ansiForeground("95A3B8"),
		fgAccent: ansiForeground("8FB3FF"),
		fgWarn:   ansiForeground("FF5F5F"),
		fgUser:   ansiForeground("E6EDF3"),
		bgUser:   ansiBackground("1C1C1C"),
	}
}

func ansiForeground(hex string) string {
	r, g, b := rgbFromHex(hex)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

func ansiBackground(hex string) string {
	r, g, b := rgbFromHex(hex)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

func rgbFromHex(hex string) (int64, int64, int64) {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return 255, 255, 255
	}
	r, rErr := strconv.ParseInt(hex[0:2], 16, 64)
	g, gErr := strconv.ParseInt(hex[2:4], 16, 64)
	b, bErr := strconv.ParseInt(hex[4:6], 16, 64)
	if rErr != nil || gErr != nil || bErr != nil {
		return 255, 255, 255
	}
	return r, g, b
}

func (t simpleTheme) style(text string, codes ...string) string {
	if !t.enabled || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + t.reset
}

func terminalWidth() int {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}
