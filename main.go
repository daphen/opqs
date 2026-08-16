package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type wireMessage struct {
	Type       string `json:"type"`
	Nonce      string `json:"nonce,omitempty"`
	Query      string `json:"query,omitempty"`
	ItemID     string `json:"item_id,omitempty"`
	FieldKind  string `json:"field_kind,omitempty"`
	FieldLabel string `json:"field_label,omitempty"`
}

type wireEvent struct {
	Type        string       `json:"type"`
	Nonce       string       `json:"nonce,omitempty"`
	Target      string       `json:"target,omitempty"`
	Status      string       `json:"status,omitempty"`
	Message     string       `json:"message,omitempty"`
	Suggestions []suggestion `json:"suggestions,omitempty"`
}

type client struct {
	conn net.Conn
	mu   sync.Mutex
	ui   bool
}

func (c *client) send(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return json.NewEncoder(c.conn).Encode(v)
}

type selection struct {
	ItemID  string
	VaultID string
	Field   fieldRequest
}

type activeSession struct {
	Nonce     string
	Target    windowInfo
	Expires   time.Time
	Pending   *selection
	Injecting bool
}

type server struct {
	store       metadataStore
	mu          sync.Mutex
	clients     map[*client]struct{}
	session     *activeSession
	metaStatus  string
	metaMessage string
}

func socketPath() string {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = filepath.Join(os.TempDir(), "opqs-"+os.Getenv("UID"))
	}
	return filepath.Join(runtime, "opqs.sock")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "summon" {
		if summon() != nil {
			os.Exit(1)
		}
		return
	}
	if runDaemon() != nil {
		os.Exit(1)
	}
}

func runDaemon() error {
	path := socketPath()
	_ = os.Remove(path)
	old := syscallUmask(0o077)
	ln, err := net.Listen("unix", path)
	syscallUmask(old)
	if err != nil {
		return err
	}
	defer ln.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	s := &server{clients: make(map[*client]struct{}), metaStatus: "loading"}
	go s.refreshLoop()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		c := &client{conn: conn}
		s.mu.Lock()
		s.clients[c] = struct{}{}
		s.mu.Unlock()
		go s.serve(c)
	}
}

var syscallUmask = syscall.Umask

func (s *server) refreshLoop() {
	s.refresh()
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		s.refresh()
	}
}

func (s *server) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := s.store.refresh(ctx)
	cancel()
	if err != nil {
		s.mu.Lock()
		s.metaStatus = "stale"
		s.metaMessage = "Unlock 1Password and enable CLI integration"
		s.mu.Unlock()
		s.broadcast(wireEvent{Type: "status", Status: "stale", Message: "Unlock 1Password and enable CLI integration"})
		return
	}
	s.mu.Lock()
	s.metaStatus = "ready"
	s.metaMessage = ""
	s.mu.Unlock()
	s.broadcast(wireEvent{Type: "status", Status: "ready"})
	s.sendCurrentResults("")
}

func (s *server) serve(c *client) {
	defer func() { c.conn.Close(); s.mu.Lock(); delete(s.clients, c); s.mu.Unlock() }()
	scan := bufio.NewScanner(c.conn)
	scan.Buffer(make([]byte, 4096), 64<<10)
	for scan.Scan() {
		var msg wireMessage
		if json.Unmarshal(scan.Bytes(), &msg) != nil {
			continue
		}
		s.handle(c, msg)
	}
}

func (s *server) handle(c *client, msg wireMessage) {
	switch msg.Type {
	case "hello":
		c.ui = true
		s.mu.Lock()
		session := s.session
		s.mu.Unlock()
		if session != nil && time.Now().Before(session.Expires) {
			c.send(s.showEvent(session, ""))
		}
	case "summon":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		target, err := captureFocusedWindow(ctx)
		cancel()
		if err != nil {
			c.send(wireEvent{Type: "ack", Status: "error", Message: "No safe input target is focused"})
			return
		}
		session := &activeSession{Nonce: randomNonce(), Target: target, Expires: time.Now().Add(60 * time.Second)}
		s.mu.Lock()
		s.session = session
		s.mu.Unlock()
		c.send(wireEvent{Type: "ack", Status: "ready"})
		s.broadcastUI(s.showEvent(session, ""))
	case "search":
		if !s.validNonce(msg.Nonce) {
			return
		}
		c.send(wireEvent{Type: "results", Nonce: msg.Nonce, Suggestions: s.store.search(msg.Query, 50)})
	case "refresh":
		if !s.validNonce(msg.Nonce) {
			return
		}
		go s.refresh()
	case "cancel":
		s.mu.Lock()
		if s.session != nil && s.session.Nonce == msg.Nonce && !s.session.Injecting {
			s.session = nil
		}
		s.mu.Unlock()
	case "select":
		s.selectField(c, msg)
	case "hidden":
		s.beginInjection(msg.Nonce)
	}
}

func (s *server) showEvent(session *activeSession, message string) wireEvent {
	s.mu.Lock()
	status, metadataMessage := s.metaStatus, s.metaMessage
	s.mu.Unlock()
	if message == "" {
		message = metadataMessage
	}
	return wireEvent{Type: "show", Nonce: session.Nonce, Target: session.Target.Title, Status: status, Message: message, Suggestions: s.store.search("", 50)}
}

func (s *server) validNonce(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil || s.session.Nonce != nonce || time.Now().After(s.session.Expires) {
		return false
	}
	return true
}

func (s *server) selectField(c *client, msg wireMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil || s.session.Nonce != msg.Nonce || s.session.Injecting || time.Now().After(s.session.Expires) {
		return
	}
	item, ok := s.store.find(msg.ItemID)
	if !ok {
		c.send(wireEvent{Type: "status", Status: "error", Message: "That item is no longer available"})
		return
	}
	req, ok := validateField(item, msg.FieldKind, msg.FieldLabel)
	if !ok {
		c.send(wireEvent{Type: "status", Status: "error", Message: "That field is not available"})
		return
	}
	s.session.Pending = &selection{ItemID: item.ID, VaultID: item.VaultID, Field: req}
	c.send(wireEvent{Type: "hide", Nonce: msg.Nonce})
}

func validateField(item itemMetadata, kind, label string) (fieldRequest, bool) {
	label = strings.TrimSpace(label)
	if strings.ContainsAny(label, "\r\n\x00") || len(label) > 128 {
		return fieldRequest{}, false
	}
	if kind == "custom" {
		return fieldRequest{Kind: kind, Label: label}, label != ""
	}
	for _, f := range item.Fields {
		if f.Kind == kind && (label == "" || strings.EqualFold(f.Label, label)) {
			return fieldRequest{Kind: f.Kind, Label: f.Label}, true
		}
	}
	return fieldRequest{}, false
}

func (s *metadataStore) find(id string) (itemMetadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if it.ID == id {
			return it, true
		}
	}
	return itemMetadata{}, false
}

func (s *server) beginInjection(nonce string) {
	s.mu.Lock()
	if s.session == nil || s.session.Nonce != nonce || s.session.Pending == nil || s.session.Injecting || time.Now().After(s.session.Expires) {
		s.mu.Unlock()
		return
	}
	s.session.Injecting = true
	session := *s.session
	selection := *s.session.Pending
	s.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := inject(ctx, session.Target, selection.ItemID, selection.VaultID, selection.Field)
		cancel()
		s.mu.Lock()
		if s.session != nil && s.session.Nonce == nonce {
			s.session = nil
		}
		s.mu.Unlock()
		if err != nil {
			retry := &activeSession{Nonce: randomNonce(), Target: session.Target, Expires: time.Now().Add(60 * time.Second)}
			s.mu.Lock()
			s.session = retry
			s.mu.Unlock()
			s.broadcastUI(s.showEvent(retry, err.Error()))
		} else {
			s.broadcastUI(wireEvent{Type: "done"})
		}
	}()
}

func (s *server) sendCurrentResults(query string) {
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	if session != nil {
		s.broadcastUI(wireEvent{Type: "results", Nonce: session.Nonce, Suggestions: s.store.search(query, 50)})
	}
}

func (s *server) broadcast(v wireEvent) {
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		_ = c.send(v)
	}
}
func (s *server) broadcastUI(v wireEvent) {
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		if c.ui {
			clients = append(clients, c)
		}
	}
	s.mu.Unlock()
	for _, c := range clients {
		_ = c.send(v)
	}
}

func randomNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func summon() error {
	conn, err := net.DialTimeout("unix", socketPath(), time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(wireMessage{Type: "summon"}); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ack wireEvent
	if err := json.NewDecoder(conn).Decode(&ack); err != nil {
		return err
	}
	if ack.Type != "ack" || ack.Status != "ready" {
		return errors.New("summon rejected")
	}
	return nil
}
