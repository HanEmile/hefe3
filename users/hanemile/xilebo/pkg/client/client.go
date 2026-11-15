// Package client provides a Go client for the xilebo API.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client talks to a xilebo homeserver.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	UserID     string
	Handle     string
}

// New creates a client for the given server URL (e.g. "http://localhost:8080").
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) api(method, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+"/_xb/client/v1"+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var errResp struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		json.Unmarshal(data, &errResp)
		msg := errResp.Message
		if msg == "" {
			msg = errResp.Error
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("%s", msg)
	}

	return json.RawMessage(data), nil
}

// Register creates a new account.
func (c *Client) Register(username, password string) error {
	data, err := c.api("POST", "/register", map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	return c.parseAuth(data)
}

// Login authenticates with handle and password.
func (c *Client) Login(handle, password string) error {
	data, err := c.api("POST", "/login", map[string]string{"handle": handle, "password": password})
	if err != nil {
		return err
	}
	return c.parseAuth(data)
}

func (c *Client) parseAuth(data json.RawMessage) error {
	var resp struct {
		UserID string `json:"user_id"`
		Handle string `json:"handle"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	c.UserID = resp.UserID
	c.Handle = resp.Handle
	c.Token = resp.Token
	return nil
}

// Room is a chat room.
type Room struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Topic    string `json:"topic"`
	RoomType string `json:"room_type"`
}

// ListRooms returns rooms the user has joined.
func (c *Client) ListRooms() ([]Room, error) {
	data, err := c.api("GET", "/rooms", nil)
	if err != nil {
		return nil, err
	}
	var rooms []Room
	return rooms, json.Unmarshal(data, &rooms)
}

// CreateRoom creates a new room.
func (c *Client) CreateRoom(name, topic string) (*Room, error) {
	data, err := c.api("POST", "/rooms", map[string]string{"name": name, "topic": topic})
	if err != nil {
		return nil, err
	}
	var room Room
	return &room, json.Unmarshal(data, &room)
}

// JoinRoom joins a room by ID.
func (c *Client) JoinRoom(roomID string) error {
	_, err := c.api("POST", "/rooms/"+roomID+"/join", nil)
	return err
}

// Message is a chat message.
type Message struct {
	ID       string `json:"id"`
	RoomID   string `json:"room_id"`
	Sender   string `json:"sender"`
	Content  string `json:"content"`
	ServerTS int64  `json:"server_ts"`
}

// SendMessage sends a text message to a room.
func (c *Client) SendMessage(roomID, content string) (*Message, error) {
	data, err := c.api("POST", "/rooms/"+roomID+"/send", map[string]string{"content": content})
	if err != nil {
		return nil, err
	}
	var msg Message
	return &msg, json.Unmarshal(data, &msg)
}

// GetMessages returns messages from a room.
func (c *Client) GetMessages(roomID string, limit int) ([]Message, error) {
	data, err := c.api("GET", fmt.Sprintf("/rooms/%s/messages?limit=%d", roomID, limit), nil)
	if err != nil {
		return nil, err
	}
	var msgs []Message
	return msgs, json.Unmarshal(data, &msgs)
}

// Member is a room member.
type Member struct {
	UserID     string `json:"UserID"`
	PowerLevel int    `json:"PowerLevel"`
}

// GetMembers returns members of a room.
func (c *Client) GetMembers(roomID string) ([]Member, error) {
	data, err := c.api("GET", "/rooms/"+roomID+"/members", nil)
	if err != nil {
		return nil, err
	}
	var members []Member
	return members, json.Unmarshal(data, &members)
}

// Sync fetches new events since the given token.
func (c *Client) Sync(since string) (json.RawMessage, error) {
	path := "/sync"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	return c.api("GET", path, nil)
}
