package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"
	"github.com/RohitPanda/quic-tracker/agents"
	"time"
)

const (
	AE_TLSHandshakeFailed = 1
	AE_FailedToSetECN     = 2
	AE_NonECN             = 3
	AE_NoACKECNReceived   = 4
	AE_NonECNButACKECN    = 5
)

type AckECNScenario struct {
	AbstractScenario
}

func NewAckECNScenario() *AckECNScenario {
	return &AckECNScenario{AbstractScenario{name: "ack_ecn", version: 1}}
}
func (s *AckECNScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)

	connAgents := s.CompleteHandshake(conn, trace, AE_TLSHandshakeFailed)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	socketAgent := connAgents.Get("SocketAgent").(*agents.SocketAgent)
	ecnStatus := make(chan interface{}, 1000)
	socketAgent.ECNStatus.Register(ecnStatus)

	err := socketAgent.ConfigureECN()
	if err != nil {
		trace.MarkError(AE_FailedToSetECN, err.Error(), nil)
		return
	}

	conn.SendHTTPGETRequest(preferredUrl, 0)

	trace.ErrorCode = AE_NonECN
	for {
		select {
		case i := <-incPackets:
			switch p := i.(type) {
			case qt.Framer:
				if p.Contains(qt.AckECNType) {
					if trace.ErrorCode == AE_NonECN {
						trace.ErrorCode = AE_NonECNButACKECN
					} else if trace.ErrorCode == AE_NoACKECNReceived {
						trace.ErrorCode = 0
					}
				}
			}
		case i := <-ecnStatus:
			switch i.(agents.ECNStatus) {
			case agents.ECNStatusNonECT:
			case agents.ECNStatusECT_0, agents.ECNStatusECT_1, agents.ECNStatusCE:
				if trace.ErrorCode == AE_NonECN {
					trace.ErrorCode = AE_NoACKECNReceived
				} else if trace.ErrorCode == AE_NonECNButACKECN {
					trace.ErrorCode = 0
				}
			}
		case <-s.Timeout().C:
			return
		}
	}
}
