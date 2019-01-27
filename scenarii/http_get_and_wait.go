package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"

	"fmt"
	"time"
)

const (
	SGW_TLSHandshakeFailed              = 1
	SGW_EmptyStreamFrameNoFinBit        = 2
	SGW_RetransmittedAck                = 3 // This could affect performance, but we don't check it anymore
	SGW_WrongStreamIDReceived           = 4
	SGW_UnknownError                    = 5
	SGW_DidNotCloseTheConnection        = 6
	SGW_MultipleErrors                  = 7
	SGW_TooLowStreamIdBidiToSendRequest = 8
	SGW_DidntReceiveTheRequestedData    = 9
	SGW_AnsweredOnUnannouncedStream     = 10
)

type SimpleGetAndWaitScenario struct {
	AbstractScenario
}

func NewSimpleGetAndWaitScenario() *SimpleGetAndWaitScenario {
	return &SimpleGetAndWaitScenario{AbstractScenario{name: "http_get_and_wait", version: 1}}
}

func (s *SimpleGetAndWaitScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	conn.TLSTPHandler.MaxBidiStreams = 0
	conn.TLSTPHandler.MaxUniStreams = 0

	connAgents := s.CompleteHandshake(conn, trace, SGW_TLSHandshakeFailed)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	if conn.TLSTPHandler.ReceivedParameters.MaxBidiStreams == 0 {
		trace.MarkError(SGW_TooLowStreamIdBidiToSendRequest, "cannot open bidi stream", nil)
	}

	errors := make(map[uint8]string)
	incomingPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incomingPackets)

	conn.SendHTTPGETRequest(preferredUrl, 0)

	var connectionCloseReceived bool

forLoop:
	for {
		select {
		case i := <-incomingPackets:
			switch p := i.(type) {
			case *qt.ProtectedPacket:
				for _, f := range p.GetFrames() {
					switch f := f.(type) {
					case *qt.StreamFrame:
						if f.StreamId != 0 {
							errors[SGW_WrongStreamIDReceived] = fmt.Sprintf("received StreamID %d", f.StreamId)
							trace.MarkError(SGW_WrongStreamIDReceived, "", p)
						}
						if f.Length == 0 && !f.FinBit {
							errors[SGW_EmptyStreamFrameNoFinBit] = fmt.Sprintf("received an empty STREAM frame with no FIN bit set for stream %d", f.StreamId)
							trace.MarkError(SGW_EmptyStreamFrameNoFinBit, "", p)
						}
					case *qt.ConnectionCloseFrame, *qt.ApplicationCloseFrame:
						connectionCloseReceived = true
					}
				}
			}
		case <-s.Timeout().C:
			break forLoop
		}
	}

	if conn.TLSTPHandler.ReceivedParameters.MaxBidiStreams == 0 {
		if conn.Streams.Get(0).ReadOffset > 0 {
			errors[SGW_AnsweredOnUnannouncedStream] = "data was received on stream 0 despite not being announced in TP"
		} else if !connectionCloseReceived {
			errors[SGW_DidNotCloseTheConnection] = ""
		}
	} else if !conn.Streams.Get(0).ReadClosed || conn.Streams.Get(0).ReadOffset == 0 {
		errors[SGW_DidntReceiveTheRequestedData] = "the response to the request was not complete"
	}

	if len(errors) > 1 {
		trace.ErrorCode = SGW_MultipleErrors
		trace.Results["error"] = errors
	}
}
