package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"

	"time"
)

const (
	FC_TLSHandshakeFailed          = 1
	FC_HostSentMoreThanLimit       = 2
	FC_HostDidNotResumeSending     = 3
	FC_NotEnoughDataAvailable      = 4
	FC_RespectedLimitsButNoBlocked = 5  // After discussing w/ the implementers, it is not reasonable to expect a STREAM_BLOCKED or a BLOCKED frame to be sent.
										// These frames should be sent to signal poor window size w.r.t. to the RTT
)

type FlowControlScenario struct {
	AbstractScenario
}

func NewFlowControlScenario() *FlowControlScenario {
	return &FlowControlScenario{AbstractScenario{name: "flow_control", version: 2}}
}
func (s *FlowControlScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	conn.TLSTPHandler.MaxStreamDataBidiLocal = 80

	connAgents := s.CompleteHandshake(conn, trace, FC_TLSHandshakeFailed)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	conn.SendHTTPGETRequest(preferredUrl, 0)

	var shouldResume bool

forLoop:
	for {
		select {
		case i := <-incPackets:
			p := i.(qt.Packet)
			if conn.Streams.Get(0).ReadOffset > uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal) {
				trace.MarkError(FC_HostSentMoreThanLimit, "", p)
			}

			if conn.Streams.Get(0).ReadClosed {
				conn.IncomingPackets.Unregister(incPackets)
				break
			}

			readOffset := conn.Streams.Get(0).ReadOffset
			if readOffset == uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal) && !shouldResume {
				conn.TLSTPHandler.MaxData *= 2
				conn.TLSTPHandler.MaxStreamDataBidiLocal *= 2
				conn.FrameQueue.Submit(qt.QueuedFrame{qt.MaxDataFrame{uint64(conn.TLSTPHandler.MaxData)}, qt.EncryptionLevel1RTT})
				conn.FrameQueue.Submit(qt.QueuedFrame{qt.MaxStreamDataFrame{0, uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal)}, qt.EncryptionLevel1RTT})
				shouldResume = true
			}
			case <-s.Timeout().C:
				break forLoop
		}
	}

	readOffset := conn.Streams.Get(0).ReadOffset
	if readOffset == uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal) {
		trace.ErrorCode = 0
	} else if shouldResume && readOffset == uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal)/2 {
		trace.ErrorCode = FC_HostDidNotResumeSending
	} else if readOffset < uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal) {
		trace.ErrorCode = FC_NotEnoughDataAvailable
	} else if readOffset > uint64(conn.TLSTPHandler.MaxStreamDataBidiLocal) {
		trace.ErrorCode = FC_HostSentMoreThanLimit
	}
}
