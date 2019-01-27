package scenarii

import (
	qt "github.com/RohitPanda/quic-tracker"
	"fmt"

	"time"
	"encoding/hex"
)

const (
	RCI_TLSHandshakeFailed       = 1
	RCI_HostDidNotProvideCID     = 2
	RCI_HostDidNotProvideNewCID  = 3
	RCI_HostSentInvalidCIDLength = 4
)

type RetireConnectionIDScenario struct {
	AbstractScenario
}

func NewRetireConnectionIDScenario() *RetireConnectionIDScenario {
	return &RetireConnectionIDScenario{AbstractScenario{name: "retire_connection_id", version: 1}}
}
func (s *RetireConnectionIDScenario) Run(conn *qt.Connection, trace *qt.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	connAgents := s.CompleteHandshake(conn, trace, RCI_TLSHandshakeFailed)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	trace.ErrorCode = RCI_HostDidNotProvideCID

	var alternativeConnectionIDs []string
	defer func() { trace.Results["new_connection_ids"] = alternativeConnectionIDs }()

	var hasRetiredCIDs bool

	for {
		select {
		case i := <-incPackets:
			p := i.(qt.Packet)

			if pp, ok := p.(*qt.ProtectedPacket); ok {
				for _, frame := range pp.GetAll(qt.NewConnectionIdType) {
					nci := frame.(*qt.NewConnectionIdFrame)

					if nci.Length < 4 || nci.Length > 18 {
						err := fmt.Sprintf("Connection ID length must be comprised between 4 and 18, it was %d", nci.Length)
						trace.MarkError(RCI_HostSentInvalidCIDLength, err, pp)
					}

					alternativeConnectionIDs = append(alternativeConnectionIDs, hex.EncodeToString(nci.ConnectionId))

					if !hasRetiredCIDs {
						conn.FrameQueue.Submit(qt.QueuedFrame{&qt.RetireConnectionId{nci.Sequence}, qt.EncryptionLevel1RTT})
					} else {
						trace.ErrorCode = 0
					}
				}
				if !hasRetiredCIDs && pp.Contains(qt.NewConnectionIdType) {
					hasRetiredCIDs = true
					trace.ErrorCode = RCI_HostDidNotProvideNewCID
				}
			}
		case <-s.Timeout().C:
			return
		}
	}
}
