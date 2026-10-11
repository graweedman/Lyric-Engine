package obs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// fakeOBS speaks enough obs-websocket 5 to authenticate and record. The
// record state event is sent before the request's response, as OBS may.
func fakeOBS(t *testing.T, password string, got *[]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx := context.Background()
		send := func(op int, d any) {
			b, _ := json.Marshal(map[string]any{"op": op, "d": d})
			conn.Write(ctx, websocket.MessageText, b)
		}
		read := func() (m struct {
			Op int
			D  map[string]any
		}) {
			_, b, err := conn.Read(ctx)
			if err == nil {
				json.Unmarshal(b, &m)
			}
			return
		}
		send(opHello, map[string]any{"rpcVersion": 1, "authentication": map[string]string{"challenge": "ch", "salt": "sa"}})
		if m := read(); m.D["authentication"] != authString(password, "sa", "ch") {
			conn.Close(4009, "Authentication failed.")
			return
		}
		send(opIdentified, map[string]any{"negotiatedRpcVersion": 1})
		for {
			m := read()
			if m.Op != opRequest {
				return
			}
			typ := m.D["requestType"].(string)
			*got = append(*got, typ)
			state := map[string]string{"StartRecord": "OBS_WEBSOCKET_OUTPUT_STARTED", "StopRecord": "OBS_WEBSOCKET_OUTPUT_STOPPED"}[typ]
			send(opEvent, map[string]any{"eventType": "RecordStateChanged", "eventData": map[string]string{"outputState": state}})
			send(opResponse, map[string]any{"requestType": typ, "requestId": m.D["requestId"], "requestStatus": map[string]any{"result": true, "code": 100}})
		}
	}))
}

func TestRecord(t *testing.T) {
	var got []string
	srv := fakeOBS(t, "secret", &got)
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	c, err := Connect(addr, "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.StartRecord(); err != nil {
		t.Fatal(err)
	}
	if err := c.StopRecord(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "StartRecord,StopRecord" {
		t.Fatalf("requests = %v", got)
	}
}

func TestWrongPassword(t *testing.T) {
	var got []string
	srv := fakeOBS(t, "secret", &got)
	defer srv.Close()
	_, err := Connect(strings.TrimPrefix(srv.URL, "http://"), "nope")
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v", err)
	}
}

// The example from the obs-websocket protocol docs.
func TestAuthString(t *testing.T) {
	if got := authString("supersecretpassword", "lM1GncleQOaCu9lT1yeUZhFYnqhsLLP1G5lAGo3ixaI=", "+IxH4CnCiqpX1rM9scsNynZzbOe4KhDeYcTNS3PDaeY="); got != "1Ct943GAT+6YQUUX47Ia/ncufilbe6+oD6lY+5kaCu4=" {
		t.Fatal(got)
	}
}
