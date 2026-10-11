// Package obs starts and stops OBS Studio recordings through its built-in
// WebSocket server (obs-websocket 5, OBS 28 and later).
package obs

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/coder/websocket"
)

// Protocol opcodes and the Outputs event subscription.
const (
	opHello      = 0
	opIdentify   = 1
	opIdentified = 2
	opEvent      = 5
	opRequest    = 6
	opResponse   = 7
	subOutputs   = 1 << 6
)

// Client is a connection to OBS.
type Client struct {
	conn   *websocket.Conn
	nextID int
	states map[string]bool // record states reported since the last request
}

type message struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
}

// Connect opens ws://addr and identifies, authenticating with password
// when OBS asks for one.
func Connect(addr, password string) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+addr, nil)
	if err != nil {
		return nil, fmt.Errorf("OBS not reachable at %s (is OBS running with its WebSocket server on?)", addr)
	}
	c := &Client{conn: conn, states: map[string]bool{}}

	var hello struct {
		Authentication *struct{ Challenge, Salt string } `json:"authentication"`
	}
	if err := c.expect(ctx, opHello, &hello); err != nil {
		c.Close()
		return nil, err
	}
	identify := map[string]any{"rpcVersion": 1, "eventSubscriptions": subOutputs}
	if a := hello.Authentication; a != nil {
		if password == "" {
			c.Close()
			return nil, errors.New("OBS needs a password: pass -obs-password or set OBS_WEBSOCKET_PASSWORD")
		}
		identify["authentication"] = authString(password, a.Salt, a.Challenge)
	}
	if err := c.send(ctx, opIdentify, identify); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.expect(ctx, opIdentified, nil); err != nil {
		c.Close()
		if websocket.CloseStatus(err) == 4009 {
			return nil, errors.New("OBS rejected the password")
		}
		return nil, err
	}
	return c, nil
}

func authString(password, salt, challenge string) string {
	h := sha256.Sum256([]byte(password + salt))
	secret := base64.StdEncoding.EncodeToString(h[:])
	h = sha256.Sum256([]byte(secret + challenge))
	return base64.StdEncoding.EncodeToString(h[:])
}

// StartRecord starts recording and waits until OBS reports it has started,
// so nothing played afterwards is missed.
func (c *Client) StartRecord() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.request(ctx, "StartRecord"); err != nil {
		return err
	}
	return c.waitRecordState(ctx, "OBS_WEBSOCKET_OUTPUT_STARTED")
}

// StopRecord stops recording and waits until the file is written.
func (c *Client) StopRecord() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.request(ctx, "StopRecord"); err != nil {
		return err
	}
	return c.waitRecordState(ctx, "OBS_WEBSOCKET_OUTPUT_STOPPED")
}

// Close disconnects.
func (c *Client) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
}

// request sends requestType and waits for its response, skipping events.
func (c *Client) request(ctx context.Context, requestType string) error {
	c.nextID++
	id := strconv.Itoa(c.nextID)
	clear(c.states)
	if err := c.send(ctx, opRequest, map[string]any{"requestType": requestType, "requestId": id}); err != nil {
		return err
	}
	for {
		var resp struct {
			RequestID     string `json:"requestId"`
			RequestStatus struct {
				Result  bool   `json:"result"`
				Code    int    `json:"code"`
				Comment string `json:"comment"`
			} `json:"requestStatus"`
		}
		if err := c.expect(ctx, opResponse, &resp); err != nil {
			return err
		}
		if resp.RequestID != id {
			continue
		}
		if st := resp.RequestStatus; !st.Result {
			return fmt.Errorf("OBS %s failed (%d): %s", requestType, st.Code, st.Comment)
		}
		return nil
	}
}

func (c *Client) waitRecordState(ctx context.Context, state string) error {
	for !c.states[state] {
		if err := c.expect(ctx, opEvent, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) send(ctx context.Context, op int, d any) error {
	b, err := json.Marshal(map[string]any{"op": op, "d": d})
	if err != nil {
		return err
	}
	return c.conn.Write(ctx, websocket.MessageText, b)
}

// expect reads messages until one with opcode op arrives and decodes its
// data into v (when non-nil). Record state events are noted on the way.
func (c *Client) expect(ctx context.Context, op int, v any) error {
	for {
		_, b, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		var m message
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		if m.Op == opEvent {
			var ev struct {
				EventType string `json:"eventType"`
				EventData struct {
					OutputState string `json:"outputState"`
				} `json:"eventData"`
			}
			if json.Unmarshal(m.D, &ev) == nil && ev.EventType == "RecordStateChanged" {
				c.states[ev.EventData.OutputState] = true
			}
		}
		if m.Op != op {
			continue
		}
		if v == nil {
			return nil
		}
		return json.Unmarshal(m.D, v)
	}
}
