package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/nativewire"
	"github.com/gin-gonic/gin"
)

// handleNativeClaudeStream relays the upstream SSE bytes unchanged while
// observing complete events for billing. It does not synthesize keepalives,
// normalize line endings, rewrite JSON, or insert an error event.
func (s *GatewayService) handleNativeClaudeStream(resp *http.Response, c *gin.Context, started time.Time) (*streamingResult, error) {
	nativewire.CopyResponseHeaders(c.Writer.Header(), resp.Header)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	result := &streamingResult{usage: &ClaudeUsage{}}
	compressed := resp.Header.Get("Content-Encoding") != "" && !strings.EqualFold(resp.Header.Get("Content-Encoding"), "identity")
	var observation io.Reader = resp.Body
	var wireWriter *nativeStreamWireWriter
	if compressed {
		wireWriter = &nativeStreamWireWriter{dst: c.Writer, flusher: flusher}
		var closeObservation func()
		var err error
		observation, closeObservation, err = nativewire.NewStreamObservationReader(resp.Body, resp.Header, wireWriter)
		if err != nil {
			return result, err
		}
		defer closeObservation()
	}
	reader := bufio.NewReaderSize(observation, 64*1024)
	const maxEventLine = 4 << 20
	line := make([]byte, 0, 1024)
	eventName := ""
	dataLines := make([]string, 0, 2)
	terminal := false
	clientDisconnected := false
	processEvent := func() {
		if len(dataLines) == 0 {
			eventName = ""
			return
		}
		data := strings.Join(dataLines, "\n")
		if result.firstTokenMs == nil && data != "[DONE]" {
			ms := int(time.Since(started).Milliseconds())
			result.firstTokenMs = &ms
		}
		observer.ObserveAnthropic([]byte(data))
		s.parseSSEUsage(data, result.usage)
		if anthropicStreamEventIsTerminal(eventName, data) {
			terminal = true
		}
		eventName = ""
		dataLines = dataLines[:0]
	}
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			if !compressed && !clientDisconnected {
				if _, writeErr := c.Writer.Write(fragment); writeErr != nil {
					clientDisconnected = true
					result.clientDisconnect = true
				} else {
					flusher.Flush()
				}
			}
			if len(line)+len(fragment) <= maxEventLine {
				line = append(line, fragment...)
			} else {
				return result, fmt.Errorf("native SSE line exceeds %d bytes", maxEventLine)
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 {
			text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
			switch {
			case text == "":
				processEvent()
			case strings.HasPrefix(text, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(text, "event:"))
			case strings.HasPrefix(text, "data:"):
				dataLines = append(dataLines, strings.TrimPrefix(strings.TrimPrefix(text, "data:"), " "))
			}
			line = line[:0]
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			if wireWriter != nil {
				if _, err := io.Copy(wireWriter, resp.Body); err != nil {
					return result, err
				}
			}
			if wireWriter != nil && wireWriter.disconnected {
				result.clientDisconnect = true
			}
			processEvent()
			if terminal {
				return result, nil
			}
			return result, io.ErrUnexpectedEOF
		}
		return result, readErr
	}
}
