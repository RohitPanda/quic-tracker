package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"
	"github.com/RohitPanda/quic-tracker/agents"
	"time"
)

const (
	H3G_TLSHandshakeFailed = 1
	H3G_RequestTimeout     = 2
	H3G_NotEnoughStreamsAvailable = 3
)

type HTTP3GETScenario struct {
	AbstractScenario
}

func NewHTTP3GETScenario() *HTTP3GETScenario {
	return &HTTP3GETScenario{AbstractScenario{name: "http3_get", version: 1, http3: true}}
}
func (s *HTTP3GETScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	conn.TLSTPHandler.MaxUniStreams = 3
	conn.TransitionTo(qt.QuicVersion, qt.QuicH3ALPNToken)

	http := agents.HTTPAgent{}
	connAgents := s.CompleteHandshake(conn, trace, H3G_TLSHandshakeFailed, &http)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	if conn.TLSTPHandler.ReceivedParameters.MaxUniStreams < 3 || conn.TLSTPHandler.ReceivedParameters.MaxBidiStreams == 0 {
		trace.ErrorCode = H3G_NotEnoughStreamsAvailable
		trace.Results["max_uni_streams"] = conn.TLSTPHandler.ReceivedParameters.MaxUniStreams
		trace.Results["max_bidi_streams"] = conn.TLSTPHandler.ReceivedParameters.MaxBidiStreams
		return
	}

	responseReceived := make(chan interface{}, 1000)
	http.HTTPResponseReceived.Register(responseReceived)

	http.SendRequest(preferredUrl, "GET", trace.Host, nil)

	select {
	case <-responseReceived:
		trace.ErrorCode = 0
		<-s.Timeout().C
	case <-s.Timeout().C:
		trace.ErrorCode = H3G_RequestTimeout
		return
	}
}
