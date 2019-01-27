package agents

import (
	"bytes"
	. "github.com/RohitPanda/quic-tracker"
	"unsafe"
)

// The ParsingAgent is responsible for decrypting and parsing the payloads received in UDP datagrams. It also decrypts
// the packet number if needed. Payloads that require a decryption level that is not available are put back into the
// UnprocessedPayloads queue.
type ParsingAgent struct {
	BaseAgent
	conn *Connection
}

func (a *ParsingAgent) Run(conn *Connection) {
	a.conn = conn
	a.Init("ParsingAgent", conn.OriginalDestinationCID)

	incomingPayloads := make(chan interface{})
	a.conn.IncomingPayloads.Register(incomingPayloads)

	go func() {
		defer a.Logger.Println("Agent terminated")
		defer close(a.closed)
		for {
		packetSelect:
			select {
			case i := <-incomingPayloads:
				udpPayload := i.([]byte)
				var off int
				for off < len(udpPayload) {
					ciphertext := udpPayload[off:]
					header := ReadHeader(bytes.NewReader(ciphertext), a.conn)
					cryptoState := a.conn.CryptoStates[header.EncryptionLevel()]

					if lh, ok := header.(*LongHeader); ok && lh.Version == 0x00000000 {
						packet := ReadVersionNegotationPacket(bytes.NewReader(ciphertext))

						a.SaveCleartextPacket(ciphertext, packet.Pointer())
						a.conn.IncomingPackets.Submit(packet)

						break packetSelect
					}

					switch header.PacketType() {
					case Initial, Handshake, ZeroRTTProtected, ShortHeaderPacket: // Decrypt PN
						if cryptoState != nil && cryptoState.HeaderRead != nil && cryptoState.Read != nil {
							a.Logger.Printf("Decrypting packet number of %s packet of length %d bytes", header.PacketType().String(), len(ciphertext))

							firstByteMask := byte(0x1F)
							if ciphertext[0] & 0x80 == 0x80 {
								firstByteMask = 0x0F
							}

							sample, pnOffset := GetPacketSample(header, ciphertext)
							mask := cryptoState.HeaderRead.Encrypt(sample, make([]byte, 5, 5))
							ciphertext[0] ^= mask[0] & firstByteMask

							pnLength := int(ciphertext[0] & 0x3) + 1

							for i := 0; i < pnLength; i++ {
								ciphertext[pnOffset+i] ^= mask[1+i]
							}
							header = ReadHeader(bytes.NewReader(ciphertext), a.conn) // Update PN
						} else {
							a.Logger.Printf("Packet number of %s packet of length %d bytes could not be decrypted, putting it back in waiting buffer\n", header.PacketType().String(), len(ciphertext))
							a.conn.UnprocessedPayloads.Submit(UnprocessedPayload{header.EncryptionLevel(), ciphertext})
							break packetSelect
						}
					}

					a.Logger.Printf("Successfully decrypted header {type=%s, number=%d}\n", header.PacketType().String(), header.PacketNumber())

					hLen := header.HeaderLength()
					var packet Packet
					var cleartext []byte
					switch header.PacketType() {
					case Handshake, Initial:
						lHeader := header.(*LongHeader)
						pLen := int(lHeader.Length.Value) - header.TruncatedPN().Length

						if hLen+pLen > len(ciphertext) {
							a.Logger.Printf("Payload length %d is past the %d received bytes, has PN decryption failed ? Aborting", hLen+pLen, len(ciphertext))
							break packetSelect
						}

						payload := cryptoState.Read.Decrypt(ciphertext[hLen:hLen+pLen], uint64(header.PacketNumber()), ciphertext[:hLen])
						if payload == nil {
							a.Logger.Printf("Could not decrypt packet {type=%s, number=%d}\n", header.PacketType().String(), header.PacketNumber())
							break packetSelect
						}

						cleartext = append(append(cleartext, ciphertext[:hLen]...), payload...)

						if lHeader.PacketType() == Initial {
							packet = ReadInitialPacket(bytes.NewReader(cleartext), a.conn)
						} else {
							packet = ReadHandshakePacket(bytes.NewReader(cleartext), a.conn)
						}

						off += hLen + pLen
					case ShortHeaderPacket: // Packets with a short header always include a 1-RTT protected payload.
						payload := cryptoState.Read.Decrypt(ciphertext[hLen:], uint64(header.PacketNumber()), ciphertext[:hLen])
						if payload == nil {
							a.Logger.Printf("Could not decrypt packet {type=%s, number=%d}\n", header.PacketType().String(), header.PacketNumber())
							break packetSelect
						}
						cleartext = append(append(cleartext, udpPayload[off:off+hLen]...), payload...)
						packet = ReadProtectedPacket(bytes.NewReader(cleartext), a.conn)
						off = len(udpPayload)
					case Retry:
						cleartext = ciphertext
						packet = ReadRetryPacket(bytes.NewReader(cleartext), a.conn)
						off = len(udpPayload)
					default:
						a.Logger.Printf("Packet type is unknown, the first byte is %x\n", ciphertext[0])
						break packetSelect
					}

					a.Logger.Printf("Successfully parsed packet {type=%s, number=%d, length=%d}\n", header.PacketType().String(), header.PacketNumber(), len(cleartext))

					switch packet.(type) {
					case Framer:
						if packet.Header().PacketNumber() > conn.LargestPNsReceived[packet.PNSpace()] {
							conn.LargestPNsReceived[packet.PNSpace()] = packet.Header().PacketNumber()
						}
					}

					a.conn.IncomingPackets.Submit(packet)
					a.SaveCleartextPacket(cleartext, packet.Pointer())

				}
			case <-a.close:
				return
			}
		}
	}()
}

func (a *ParsingAgent) SaveCleartextPacket(cleartext []byte, unique unsafe.Pointer) {
	if a.conn.ReceivedPacketHandler != nil {
		a.conn.ReceivedPacketHandler(cleartext, unique)
	}
}
