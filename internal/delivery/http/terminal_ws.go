package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

const (
	telnetIAC  = byte(255)
	telnetDONT = byte(254)
	telnetDO   = byte(253)
	telnetWONT = byte(252)
	telnetWILL = byte(251)
	telnetSB   = byte(250)
	telnetSE   = byte(240)
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4098,
	WriteBufferSize: 4098,
	CheckOrigin:     terminalCheckOrigin,
}

func terminalCheckOrigin(r *http.Request) bool {
	o := strings.TrimSpace(r.Header.Get("Origin"))
	if o == "" {
		return true
	}
	ou, err := url.Parse(o)
	if err != nil {
		return false
	}
	rh := r.Host
	if !strings.Contains(rh, ":") || strings.HasPrefix(rh, "[") {
		return strings.EqualFold(ou.Host, rh)
	}
	// IPv4 host:port — сравниваем host без порта с Origin (часто без порта).
	if h, _, err := net.SplitHostPort(rh); err == nil {
		return strings.EqualFold(ou.Hostname(), h)
	}
	return strings.EqualFold(ou.Host, rh)
}

type terminalInitMsg struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type terminalAckMsg struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

func terminalKindFromQuery(r *http.Request) string {
	k := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	switch k {
	case "telnet":
		return "telnet"
	default:
		return "ssh"
	}
}

func deviceDialAddr(host string, port int) string {
	h := strings.TrimSpace(host)
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return net.JoinHostPort(h, strconv.Itoa(port))
}

func terminalDialTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_DIAL_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 20 * time.Second
}

func terminalSessionDeadline() time.Time {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_SESSION_MAX")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return time.Now().Add(d)
		}
	}
	return time.Now().Add(8 * time.Hour)
}

// TerminalWS: WebSocket к SSH или Telnet устройства (только admin, cookie-сессия).
// Первое сообщение — текст JSON: {"type":"init","username":"...","password":"...","port":22}.
// Далее: бинарные кадры — ввод в PTY/TCP; текст JSON {"type":"resize","cols":n,"rows":m} для SSH.
// С сервера: бинарные кадры — вывод терминала; при ошибке — текст JSON {"type":"error",...}.
func (h *Handlers) TerminalWS(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "bad device id", http.StatusBadRequest)
		return
	}
	kind := terminalKindFromQuery(r)

	dev, err := h.repo.GetDeviceByID(id)
	if err != nil || dev == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Debug("terminal ws upgrade", zap.Error(err))
		return
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	_, initRaw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var init terminalInitMsg
	if err := json.Unmarshal(initRaw, &init); err != nil || init.Type != "init" {
		_ = wsWriteText(conn, nil, terminalJSON("error", "expected init json"))
		return
	}

	port := init.Port
	if port <= 0 {
		if kind == "telnet" {
			port = 23
		} else {
			port = 22
		}
	}
	addr := deviceDialAddr(dev.IP, port)
	dialTimeout := terminalDialTimeout()
	deadline := terminalSessionDeadline()

	u := userFromContext(r.Context())
	nmsUser := ""
	if u != nil {
		nmsUser = u.username
	}
	h.logger.Info("terminal session start",
		zap.String("nms_user", nmsUser),
		zap.Int("device_id", id),
		zap.String("device_ip", dev.IP),
		zap.String("kind", kind),
		zap.String("dial_addr", addr),
	)

	if kind == "telnet" {
		if err := h.runTerminalTelnet(r.Context(), conn, addr, dialTimeout, deadline); err != nil {
			h.logger.Warn("terminal telnet ended", zap.Error(err))
			_ = wsWriteText(conn, nil, terminalJSON("error", err.Error()))
		}
		h.logger.Info("terminal session end",
			zap.String("nms_user", nmsUser),
			zap.Int("device_id", id),
			zap.String("kind", kind),
		)
		return
	}

	if strings.TrimSpace(init.Username) == "" {
		_ = wsWriteText(conn, nil, terminalJSON("error", "ssh username required"))
		return
	}

	if err := h.runTerminalSSH(r.Context(), conn, addr, init.Username, init.Password, dialTimeout, deadline); err != nil {
		h.logger.Warn("terminal ssh ended", zap.Error(err))
		_ = wsWriteText(conn, nil, terminalJSON("error", err.Error()))
	}

	h.logger.Info("terminal session end",
		zap.String("nms_user", nmsUser),
		zap.Int("device_id", id),
		zap.String("kind", kind),
	)
}

func terminalJSON(t, msg string) []byte {
	b, _ := json.Marshal(terminalAckMsg{Type: t, Message: msg})
	return b
}

func (h *Handlers) runTerminalSSH(ctx context.Context, conn *websocket.Conn, addr, user, pass string, dialTimeout time.Duration, deadline time.Time) error {
	var writeMu sync.Mutex
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         dialTimeout,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 80, 24, modes); err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		return fmt.Errorf("shell: %w", err)
	}

	_ = wsWriteText(conn, &writeMu, terminalJSON("ok", ""))

	var wg sync.WaitGroup
	wg.Add(3)
	errCh := make(chan error, 3)

	go func() {
		defer wg.Done()
		for {
			if time.Now().After(deadline) {
				errCh <- context.DeadlineExceeded
				return
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
			mt, payload, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if mt == websocket.TextMessage {
				var rs terminalResizeMsg
				if json.Unmarshal(payload, &rs) == nil && rs.Type == "resize" && rs.Cols > 0 && rs.Rows > 0 {
					_ = sess.WindowChange(rs.Rows, rs.Cols)
				}
				continue
			}
			if mt == websocket.BinaryMessage {
				if _, werr := stdin.Write(payload); werr != nil {
					errCh <- werr
					return
				}
			}
		}
	}()

	go func() {
		defer wg.Done()
		errCh <- copyReaderToWSBinary(ctx, conn, &writeMu, stdout, deadline)
	}()
	go func() {
		defer wg.Done()
		errCh <- copyReaderToWSBinary(ctx, conn, &writeMu, stderr, deadline)
	}()

	wg.Wait()
	close(errCh)
	var first error
	for e := range errCh {
		if e != nil && !errors.Is(e, io.EOF) && !websocket.IsCloseError(e, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			if first == nil {
				first = e
			}
		}
	}
	return first
}

func (h *Handlers) runTerminalTelnet(ctx context.Context, conn *websocket.Conn, addr string, dialTimeout time.Duration, deadline time.Time) error {
	var writeMu sync.Mutex
	d := net.Dialer{Timeout: dialTimeout}
	tcp, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp dial: %w", err)
	}
	defer tcp.Close()

	_ = wsWriteText(conn, &writeMu, terminalJSON("ok", ""))

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)

	go func() {
		defer wg.Done()
		errCh <- copyWSBinaryToWriter(ctx, conn, tcp, deadline)
	}()
	go func() {
		defer wg.Done()
		errCh <- copyTelnetServerToWS(ctx, conn, &writeMu, tcp, deadline)
	}()

	wg.Wait()
	close(errCh)
	var first error
	for e := range errCh {
		if e != nil && !errors.Is(e, io.EOF) && !websocket.IsCloseError(e, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			if first == nil {
				first = e
			}
		}
	}
	return first
}

func copyWSBinaryToWriter(ctx context.Context, conn *websocket.Conn, w io.Writer, deadline time.Time) error {
	for {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if mt == websocket.TextMessage {
			var rs terminalResizeMsg
			if json.Unmarshal(data, &rs) == nil && rs.Type == "resize" {
				continue
			}
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
}

func copyReaderToWSBinary(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, r io.Reader, deadline time.Time) error {
	buf := make([]byte, 8192)
	for {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			if werr := wsWriteBinary(conn, writeMu, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// copyTelnetServerToWS: поток с устройства, отвечаем отказом на опции Telnet (минимум для стабильности).
func copyTelnetServerToWS(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, tcpConn net.Conn, deadline time.Time) error {
	br := newByteReader(tcpConn)
	buf := make([]byte, 0, 4096)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		err := wsWriteBinary(conn, writeMu, buf)
		buf = buf[:0]
		return err
	}
	for {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		b, err := br.ReadByte()
		if err != nil {
			_ = flush()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if b != telnetIAC {
			buf = append(buf, b)
			if len(buf) >= 4096 {
				if err := flush(); err != nil {
					return err
				}
			}
			continue
		}
		if err := flush(); err != nil {
			return err
		}
		cmd, err := br.ReadByte()
		if err != nil {
			return err
		}
		if cmd == telnetIAC {
			buf = append(buf, telnetIAC)
			continue
		}
		switch cmd {
		case telnetDO, telnetDONT:
			opt, err := br.ReadByte()
			if err != nil {
				return err
			}
			_, _ = tcpConn.Write([]byte{telnetIAC, telnetWONT, opt})
		case telnetWILL, telnetWONT:
			opt, err := br.ReadByte()
			if err != nil {
				return err
			}
			_, _ = tcpConn.Write([]byte{telnetIAC, telnetDONT, opt})
		case telnetSB:
			for {
				x, err := br.ReadByte()
				if err != nil {
					return err
				}
				if x == telnetIAC {
					y, err := br.ReadByte()
					if err != nil {
						return err
					}
					if y == telnetSE {
						break
					}
				}
			}
		default:
			// GA, NOP, etc. — игнор.
		}
	}
}

func wsWriteText(conn *websocket.Conn, mu *sync.Mutex, payload []byte) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func wsWriteBinary(conn *websocket.Conn, mu *sync.Mutex, payload []byte) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}

type byteReader struct {
	r   io.Reader
	buf [1]byte
}

func newByteReader(r io.Reader) *byteReader { return &byteReader{r: r} }

func (b *byteReader) ReadByte() (byte, error) {
	_, err := io.ReadFull(b.r, b.buf[:])
	return b.buf[0], err
}

// TerminalPage — HTML с xterm для /devices/{id}/terminal.
func (h *Handlers) TerminalPage(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "bad device id", http.StatusBadRequest)
		return
	}
	dev, err := h.repo.GetDeviceByID(id)
	if err != nil || dev == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	kind := terminalKindFromQuery(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		DeviceID int
		Name     string
		IP       string
		Kind     string
	}{
		DeviceID: id,
		Name:     dev.Name,
		IP:       dev.IP,
		Kind:     kind,
	}
	if err := h.terminalTmpl.Execute(w, data); err != nil {
		h.logger.Error("terminal page", zap.Error(err))
	}
}

