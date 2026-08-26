-- LToUDP ("LocalTalk over UDP") Wireshark dissector.
--
-- Wire format reference:
--   adapter/link/ltoudp/{ltoudp.go,doc.go}      (the 4-byte sender ID envelope)
--   adapter/link/framing/localtalk.go           (LLAP framing + short/long DDP)
--   core/protocol/llap/frame.go                 (LLAP header + type codes)
--   core/protocol/ddp/ddp.go                    (long DDP header)
--   spec/07-port-ltoudp.md, spec/09-port-localtalk-base.md
--
-- LToUDP tunnels LocalTalk (LLAP) frames over an IPv4 multicast group
-- (239.192.76.84:1954). Each UDP datagram is:
--
--   offset 0-3    Sender ID:  4 bytes, a per-process tag (the sender's PID,
--                             big-endian) so a participant can drop its own
--                             multicast echo. NOT part of LLAP/DDP.
--   offset 4      LLAP dst:   destination node (0xFF = broadcast)
--   offset 5      LLAP src:   source node
--   offset 6      LLAP type:  0x01 short-header DDP, 0x02 long-header DDP,
--                             0x81 ENQ, 0x82 ACK (node-claim control frames)
--   offset 7+     Payload:    DDP datagram (short or long header) for the DDP
--                             types; nothing for ENQ/ACK.
--
-- SHORT-header DDP (type 0x01) omits net numbers + node addresses (they are
-- implicit: the nodes ride in the LLAP header, the network is the segment's):
--   length(2, top 2 bits of byte 0) + destSocket(1) + srcSocket(1) + ddpType(1)
--   + data. LONG-header DDP (type 0x02) carries the full 13-byte DDP header.
--
-- Install with: Wireshark -> Help -> About Wireshark -> Folders -> Personal Lua
-- Plugins, then copy this file there (or symlink it), and Analyze -> Reload Lua
-- Plugins. It binds to UDP port 1954 by default (see registration at the foot).
--
-- After the 4-byte Sender ID, the rest of the datagram is a plain LLAP frame,
-- so we hand it off to Wireshark's own built-in LocalTalk (LLAP) dissector
-- (part of epan/dissectors/packet-atalk.c, registered as "llap") instead of
-- re-implementing LLAP/DDP/ATP/ASP/AFP parsing ourselves. This mirrors the
-- draft native packet-ltoudp.c dissector for LToUDP itself
-- (wireshark/wireshark@c0e48a1a19df1ff19a5739ec36bd23e19831422b), which
-- resolves the "llap" dissector via find_dissector_add_dependency() and
-- hands off with call_dissector() the same way. Credit to @NJRoadfan for
-- pointing out that Wireshark's AppleTalk dissector already has an LLAP
-- entry point, saving us from hand-rolling it here.

local ltoudp = Proto("ltoudp", "LToUDP (LocalTalk over UDP)")

local LTOUDP_PORT = 1954
local SENDER_ID_LEN = 4

-- Retrieve Wireshark's built-in LocalTalk (LLAP) dissector
local llap_dissector = Dissector.get("llap")

-- Fields
local f_senderid = ProtoField.uint32("ltoudp.senderid", "Sender ID (PID)", base.DEC)

ltoudp.fields = { f_senderid }

-- Main dissector
function ltoudp.dissector(buf, pinfo, tree)
    -- Must have at least the 4-byte Sender ID
    if buf:len() < SENDER_ID_LEN then
        return 0
    end

    pinfo.cols.protocol = "LToUDP"

    -- Add LToUDP header to tree
    local subtree = tree:add(ltoudp, buf(0, SENDER_ID_LEN), "LToUDP")
    subtree:add(f_senderid, buf(0, SENDER_ID_LEN))

    -- Extract LLAP frame (bytes starting from offset 4)
    local llap_tvb = buf(SENDER_ID_LEN):tvb()

    -- Call Wireshark's built-in LLAP dissector.
    -- This handles LLAP + DDP + ATP + ASP + AFP automatically.
    llap_dissector:call(llap_tvb, pinfo, tree)

    return buf:len()
end

-- Register on UDP port 1954
local udp_table = DissectorTable.get("udp.port")
udp_table:add(LTOUDP_PORT, ltoudp)