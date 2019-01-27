package agents

import (
	. "github.com/RohitPanda/quic-tracker"
	"time"
	"sort"
)

// The SendingAgent is responsible of bundling the frames queued for sending into packets. If the frames queued for a
// given encryption level are smaller than a given MTU, it will wait a window of 5ms before sending them in the hope
// that more will be queued. Frames that require an unavailable encryption level are queued until it is made available.
// It also merge the ACK frames inside a given packet before sending.
type SendingAgent struct {
	BaseAgent
	MTU uint16
}

func (a *SendingAgent) Run(conn *Connection) {
	a.Init("SendingAgent", conn.OriginalDestinationCID)

	frameQueue := make(chan interface{}, 1000)
	conn.FrameQueue.Register(frameQueue)
	newEncryptionLevelAvailable := make(chan interface{}, 10)
	conn.EncryptionLevelsAvailable.Register(newEncryptionLevelAvailable)

	encryptionLevels := []EncryptionLevel{EncryptionLevelInitial, EncryptionLevel0RTT, EncryptionLevelHandshake, EncryptionLevel1RTT, EncryptionLevelBest, EncryptionLevelBestAppData}
	encryptionLevelsAvailable := map[DirectionalEncryptionLevel]bool {
		{EncryptionLevelNone, false}: true,
		{EncryptionLevelInitial, false}: true,
	}
	packetBuffer := make(map[EncryptionLevel][]Packet)
	frameBuffer := make(map[EncryptionLevel][]Frame)
	frameBufferLength := make(map[EncryptionLevel]uint16)
	timers := make(map[EncryptionLevel]*time.Timer)
	for _, el := range encryptionLevels {
		frameBuffer[el] = nil
		frameBufferLength[el] = 0
		timers[el] = time.NewTimer(0)
		if !timers[el].Stop() {
			<-timers[el].C
		}
	}

	fillWithLevel := func(packet Framer, level EncryptionLevel) {
		var ackFrames []*AckFrame

		for _, f := range frameBuffer[level] {
			switch f := f.(type) {
			case *AckFrame:
				ackFrames = append(ackFrames, f)
			default:
				packet.AddFrame(f)
			}
		}
		if len(ackFrames) > 1 {
			ack := mergeAckFrames(ackFrames)
			packet.AddFrame(ack)
			a.Logger.Printf("Merging %d ACK frames into 1\n", len(ackFrames))
		} else if len(ackFrames) == 1 {
			packet.AddFrame(ackFrames[0])
		}
		frameBuffer[level] = nil
		frameBufferLength[level] = 0
	}

	fillAndSendPacket := func(packet Framer, level EncryptionLevel) {
		if len(frameBuffer[level]) > 0 && encryptionLevelsAvailable[DirectionalEncryptionLevel{level, false}] {
			a.Logger.Printf("Timer for encryption level %s fired, scheduling the sending of %d bytes in %d frames\n", level.String(), frameBufferLength[level], len(frameBuffer[level]))

			fillWithLevel(packet, level)
			conn.SendPacket(packet, level)
		}
	}

	go func() {
		defer a.Logger.Println("Agent terminated")
		defer close(a.closed)
		for {
			select {
			case i := <-frameQueue:
				qf := i.(QueuedFrame)
				a.Logger.Printf("Received a %d-byte long frame requiring encryption level %s\n", qf.FrameLength(), qf.EncryptionLevel.String())
				if frameBufferLength[qf.EncryptionLevel]+qf.FrameLength() > a.MTU {
					a.Logger.Printf("Scheduling the sending of %d bytes in %d frames in the buffer\n", frameBufferLength[qf.EncryptionLevel], len(frameBuffer[qf.EncryptionLevel]))

					if qf.EncryptionLevel == EncryptionLevelBest || qf.EncryptionLevel == EncryptionLevelBestAppData {
						qf.EncryptionLevel = chooseBestEncryptionLevel(encryptionLevelsAvailable, qf.EncryptionLevel == EncryptionLevelBestAppData)
						a.Logger.Printf("Chose %s as best encryption level\n", qf.EncryptionLevel.String())
					}

					var packet Framer
					switch qf.EncryptionLevel {
					case EncryptionLevelInitial:
						packet = NewInitialPacket(conn)
					case EncryptionLevel0RTT:
						packet = NewZeroRTTProtectedPacket(conn)
					case EncryptionLevelHandshake:
						packet = NewHandshakePacket(conn)
					case EncryptionLevel1RTT:
						packet = NewProtectedPacket(conn)
					}

					fillWithLevel(packet, qf.EncryptionLevel)

					if len(packet.GetFrames()) == 0 {
						packet.AddFrame(qf.Frame) // TODO: We have a bigger frame than MTU in this case
					} else {
						frameBuffer[qf.EncryptionLevel] = append(frameBuffer[qf.EncryptionLevel], qf.Frame)  // TODO: The frame could still be sent alone
					}

					if !encryptionLevelsAvailable[DirectionalEncryptionLevel{qf.EncryptionLevel, false}] {
						a.Logger.Printf("Encryption level %s is not available, putting the packet in the buffer\n", qf.EncryptionLevel.String())
						packetBuffer[packet.EncryptionLevel()] = append(packetBuffer[packet.EncryptionLevel()], packet)
					} else {
						conn.SendPacket(packet, qf.EncryptionLevel)
					}
				} else {
					frameBuffer[qf.EncryptionLevel] = append(frameBuffer[qf.EncryptionLevel], qf.Frame)
					frameBufferLength[qf.EncryptionLevel] += qf.FrameLength()
					if encryptionLevelsAvailable[DirectionalEncryptionLevel{qf.EncryptionLevel, false}] || qf.EncryptionLevel == EncryptionLevelBest || qf.EncryptionLevel == EncryptionLevelBestAppData {
						timers[qf.EncryptionLevel].Reset(5 * time.Millisecond)
					}
				}
			case <-timers[EncryptionLevelInitial].C:
				fillAndSendPacket(NewInitialPacket(conn), EncryptionLevelInitial)
			case <-timers[EncryptionLevel0RTT].C:
				fillAndSendPacket(NewZeroRTTProtectedPacket(conn), EncryptionLevel0RTT)
			case <-timers[EncryptionLevelHandshake].C:
				fillAndSendPacket(NewHandshakePacket(conn), EncryptionLevelHandshake)
			case <-timers[EncryptionLevel1RTT].C:
				fillAndSendPacket(NewProtectedPacket(conn), EncryptionLevel1RTT)
			case <-timers[EncryptionLevelBest].C:
				eL := chooseBestEncryptionLevel(encryptionLevelsAvailable, false)
				a.Logger.Printf("Timer for encryption level %s fired, chose %s as new encryption level\n", EncryptionLevelBest.String(), eL.String())

				for _, f := range frameBuffer[EncryptionLevelBest] {
					frameBuffer[eL] = append(frameBuffer[eL], f)
					frameBufferLength[eL] += f.FrameLength()
				}
				frameBuffer[EncryptionLevelBest] = nil
				frameBufferLength[EncryptionLevelBest] = 0

				timers[eL].Reset(0)
			case <-timers[EncryptionLevelBestAppData].C:
				eL := chooseBestEncryptionLevel(encryptionLevelsAvailable, true)
				a.Logger.Printf("Timer for encryption level %s fired, chose %s as new encryption level\n", EncryptionLevelBestAppData.String(), eL.String())

				for _, f := range frameBuffer[EncryptionLevelBestAppData] {
					frameBuffer[eL] = append(frameBuffer[eL], f)
					frameBufferLength[eL] += f.FrameLength()
				}
				frameBuffer[EncryptionLevelBestAppData] = nil
				frameBufferLength[EncryptionLevelBestAppData] = 0

				timers[eL].Reset(0)
			case i := <- newEncryptionLevelAvailable:
				dEL := i.(DirectionalEncryptionLevel)
				if dEL.Read {
					continue
				}
				eL := dEL.EncryptionLevel
				encryptionLevelsAvailable[dEL] = true

				if len(packetBuffer[eL]) > 0 {
					a.Logger.Printf("Encryption level %s is available, sending %d packet(s)\n", eL.String(), len(packetBuffer[eL]))
				}

				for _, p := range packetBuffer[eL] {
					conn.SendPacket(p, eL)
				}
				packetBuffer[eL] = nil
				timers[eL].Reset(0)
			case <-a.close:
					return
			}
		}
	}()
}

var elOrder = []DirectionalEncryptionLevel {{EncryptionLevel1RTT, false}, {EncryptionLevel0RTT, false}, {EncryptionLevelHandshake, false}, {EncryptionLevelInitial, false}}
var elAppDataOrder = []DirectionalEncryptionLevel {{EncryptionLevel1RTT, false}, {EncryptionLevel0RTT, false}}

func chooseBestEncryptionLevel(elAvailable map[DirectionalEncryptionLevel]bool, restrictAppData bool) EncryptionLevel {
	order := elOrder
	if restrictAppData {
		order = elAppDataOrder
	}
	for _, dEL := range order {
		if elAvailable[dEL] {
			return dEL.EncryptionLevel
		}
	}
	if restrictAppData {
		return EncryptionLevel1RTT
	}
	return order[len(order) - 1].EncryptionLevel
}

func mergeAckFrames(frames []*AckFrame) *AckFrame {
	result := new(AckFrame)

	for _, f := range frames {
		if f.LargestAcknowledged > result.LargestAcknowledged {
			result.LargestAcknowledged = f.LargestAcknowledged
		}
	}

	numbersAcknowledged := make(map[PacketNumber]bool)

	for _, f := range frames {
		offset := uint64(0)
		numbersAcknowledged[f.LargestAcknowledged] = true
		for bidx, b := range f.AckBlocks {
			for i := uint64(0); i < b.Block; i++ {
				numbersAcknowledged[PacketNumber(uint64(f.LargestAcknowledged) - i - offset)] = true
			}
			for i := uint64(0); i <= b.Gap && bidx > 0; i++ {
				offset += 1
			}
		}
	}

	packetNumbers := make([]PacketNumber, 0, len(numbersAcknowledged))

	for n := range numbersAcknowledged {
		packetNumbers = append(packetNumbers, n)
	}

	sort.Sort(PacketNumberQueue(packetNumbers))

	previous := result.LargestAcknowledged
	ackBlock := AckBlock{}
	for _, number := range packetNumbers[1:] {
		if previous - number == 1 {
			ackBlock.Block++
		} else {
			result.AckBlocks = append(result.AckBlocks, ackBlock)
			ackBlock = AckBlock{Gap: uint64(previous) - uint64(number) - 1}
		}
		previous = number
	}
	result.AckBlocks = append(result.AckBlocks, ackBlock)
	if len(result.AckBlocks) > 0 {
		result.AckBlockCount = uint64(len(result.AckBlocks) - 1)
	}

	return result
}