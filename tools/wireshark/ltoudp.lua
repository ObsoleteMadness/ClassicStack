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

local ltoudp = Proto("ltoudp", "LToUDP (LocalTalk over UDP)")

local LTOUDP_PORT = 1954
local SENDER_ID_LEN = 4
local LLAP_HDR_LEN = 3

-- LLAP type codes (core/protocol/llap/frame.go).
local LLAP_SHORT_DDP = 0x01
local LLAP_LONG_DDP  = 0x02
local LLAP_ENQ       = 0x81
local LLAP_ACK       = 0x82

local llap_types = {
    [LLAP_SHORT_DDP] = "Short-header DDP",
    [LLAP_LONG_DDP]  = "Long-header DDP",
    [LLAP_ENQ]       = "ENQ (node-claim probe)",
    [LLAP_ACK]       = "ACK (node-claim defend)",
}

-- DDP well-known socket numbers (spec/00-overview.md; RTMP=1, NBP=2, ATP=3(EP),
-- ZIP=6). These are the statically assigned ones; dynamic sockets (128-254) are
-- shown as their raw value.
local ddp_sockets = {
    [1] = "RTMP",
    [2] = "NBP",
    [4] = "Echo",
    [6] = "ZIP",
}

-- DDP protocol types (the "DDP type" byte; spec/00-overview.md).
local ddp_ptypes = {
    [1]    = "RTMP Data",
    [2]    = "NBP",
    [3]    = "ATP",
    [4]    = "Echo",
    [5]    = "RTMP Request",
    [6]    = "ZIP",
    [22]   = "MacIP (IP-in-DDP)",
    [0x4E] = "MacIPX Gateway",
}

-- ---------------------------------------------------------------------------
-- Fields
-- ---------------------------------------------------------------------------
local f_senderid   = ProtoField.uint32("ltoudp.senderid", "Sender ID (PID)", base.DEC)

local f_llap_dst   = ProtoField.uint8("ltoudp.llap.dst", "LLAP destination node", base.DEC_HEX)
local f_llap_src   = ProtoField.uint8("ltoudp.llap.src", "LLAP source node", base.DEC_HEX)
local f_llap_type  = ProtoField.uint8("ltoudp.llap.type", "LLAP type", base.HEX, llap_types)

-- DDP header (fields common to short/long; long adds net numbers + nodes).
local f_ddp_hops     = ProtoField.uint8("ltoudp.ddp.hops", "Hop count", base.DEC)
local f_ddp_length   = ProtoField.uint16("ltoudp.ddp.length", "Length", base.DEC)
local f_ddp_checksum = ProtoField.uint16("ltoudp.ddp.checksum", "Checksum", base.HEX)
local f_ddp_dstnet   = ProtoField.uint16("ltoudp.ddp.dstnet", "Destination network", base.DEC)
local f_ddp_srcnet   = ProtoField.uint16("ltoudp.ddp.srcnet", "Source network", base.DEC)
local f_ddp_dstnode  = ProtoField.uint8("ltoudp.ddp.dstnode", "Destination node", base.DEC_HEX)
local f_ddp_srcnode  = ProtoField.uint8("ltoudp.ddp.srcnode", "Source node", base.DEC_HEX)
local f_ddp_dstsock  = ProtoField.uint8("ltoudp.ddp.dstsocket", "Destination socket", base.DEC, ddp_sockets)
local f_ddp_srcsock  = ProtoField.uint8("ltoudp.ddp.srcsocket", "Source socket", base.DEC, ddp_sockets)
local f_ddp_type     = ProtoField.uint8("ltoudp.ddp.type", "DDP type", base.DEC, ddp_ptypes)
local f_ddp_data     = ProtoField.bytes("ltoudp.ddp.data", "DDP data")

ltoudp.fields = {
    f_senderid,
    f_llap_dst, f_llap_src, f_llap_type,
    f_ddp_hops, f_ddp_length, f_ddp_checksum,
    f_ddp_dstnet, f_ddp_srcnet, f_ddp_dstnode, f_ddp_srcnode,
    f_ddp_dstsock, f_ddp_srcsock, f_ddp_type, f_ddp_data,
}

-- ---------------------------------------------------------------------------
-- DDP dissection. buf covers the DDP datagram (i.e. the LLAP payload); dst_node
-- and src_node are taken from the LLAP header for the short-header form, which
-- omits them. Returns the info-column tail describing sockets.
-- ---------------------------------------------------------------------------

-- SHORT-header DDP (LLAP type 0x01): length(2) + dstSocket + srcSocket + ddpType
-- + data. Net numbers/nodes are implicit (spec/09 §"Short-header DDP"); the nodes
-- come from the LLAP header, the network from the receiving segment (not on wire).
local function dissect_short_ddp(buf, tree, dst_node, src_node)
    if buf:len() < 5 then
        return "short DDP (runt)"
    end
    -- Length: low 10 bits (byte 0 bits 1-0, byte 1). Byte 0 bits 7-2 are zero.
    local length = bit.bor(bit.lshift(bit.band(buf(0, 1):uint(), 0x03), 8), buf(1, 1):uint())
    local dstsock = buf(2, 1):uint()
    local srcsock = buf(3, 1):uint()

    tree:add(f_ddp_length, buf(0, 2), length)
    tree:add(buf(2, 1), string.format("Destination node (LLAP): %d (0x%02X)", dst_node, dst_node)):set_generated()
    tree:add(buf(2, 1), string.format("Source node (LLAP): %d (0x%02X)", src_node, src_node)):set_generated()
    tree:add(f_ddp_dstsock, buf(2, 1))
    tree:add(f_ddp_srcsock, buf(3, 1))
    tree:add(f_ddp_type, buf(4, 1))
    if buf:len() > 5 then
        tree:add(f_ddp_data, buf(5, buf:len() - 5))
    end
    return string.format("socket %d->%d", srcsock, dstsock)
end

-- LONG-header DDP (LLAP type 0x02): the full 13-byte DDP header (core/protocol/
-- ddp/ddp.go) then data.
local function dissect_long_ddp(buf, tree)
    if buf:len() < 13 then
        return "long DDP (runt)"
    end
    local b0 = buf(0, 1):uint()
    local hops = bit.rshift(bit.band(b0, 0x3C), 2)
    local length = bit.bor(bit.lshift(bit.band(b0, 0x03), 8), buf(1, 1):uint())
    local dstsock = buf(10, 1):uint()
    local srcsock = buf(11, 1):uint()

    tree:add(f_ddp_hops, buf(0, 1), hops)
    tree:add(f_ddp_length, buf(0, 2), length)
    tree:add(f_ddp_checksum, buf(2, 2))
    tree:add(f_ddp_dstnet, buf(4, 2))
    tree:add(f_ddp_srcnet, buf(6, 2))
    tree:add(f_ddp_dstnode, buf(8, 1))
    tree:add(f_ddp_srcnode, buf(9, 1))
    tree:add(f_ddp_dstsock, buf(10, 1))
    tree:add(f_ddp_srcsock, buf(11, 1))
    tree:add(f_ddp_type, buf(12, 1))
    if buf:len() > 13 then
        tree:add(f_ddp_data, buf(13, buf:len() - 13))
    end
    return string.format("net %d.%d->%d.%d socket %d->%d",
        buf(6, 2):uint(), buf(9, 1):uint(),
        buf(4, 2):uint(), buf(8, 1):uint(),
        srcsock, dstsock)
end

-- ---------------------------------------------------------------------------
-- Main dissector. buf is the whole UDP payload: 4-byte sender ID + LLAP frame.
-- ---------------------------------------------------------------------------
function ltoudp.dissector(buf, pinfo, tree)
    if buf:len() < SENDER_ID_LEN + LLAP_HDR_LEN then
        return 0
    end

    pinfo.cols.protocol = "LToUDP"

    local subtree = tree:add(ltoudp, buf(), "LToUDP")
    subtree:add(f_senderid, buf(0, SENDER_ID_LEN))

    local dst_node = buf(SENDER_ID_LEN, 1):uint()
    local src_node = buf(SENDER_ID_LEN + 1, 1):uint()
    local llap_type = buf(SENDER_ID_LEN + 2, 1):uint()

    local llap_tree = subtree:add(buf(SENDER_ID_LEN, LLAP_HDR_LEN), "LLAP header")
    llap_tree:add(f_llap_dst, buf(SENDER_ID_LEN, 1))
    llap_tree:add(f_llap_src, buf(SENDER_ID_LEN + 1, 1))
    llap_tree:add(f_llap_type, buf(SENDER_ID_LEN + 2, 1))

    local type_name = llap_types[llap_type] or string.format("Unknown (0x%02X)", llap_type)
    local dst_label = (dst_node == 0xFF) and "bcast" or tostring(dst_node)

    local payload_off = SENDER_ID_LEN + LLAP_HDR_LEN
    local payload_len = buf:len() - payload_off

    if llap_type == LLAP_ENQ or llap_type == LLAP_ACK then
        -- Control frames are header-only (no DDP payload).
        pinfo.cols.info = string.format("LLAP %s  node %d",
            (llap_type == LLAP_ENQ) and "ENQ" or "ACK", src_node)
    elseif llap_type == LLAP_SHORT_DDP then
        local ddp_tree = subtree:add(buf(payload_off, payload_len), "DDP (short header)")
        local tail = dissect_short_ddp(buf(payload_off, payload_len), ddp_tree, dst_node, src_node)
        pinfo.cols.info = string.format("DDP short  node %d->%s  %s", src_node, dst_label, tail)
    elseif llap_type == LLAP_LONG_DDP then
        local ddp_tree = subtree:add(buf(payload_off, payload_len), "DDP (long header)")
        local tail = dissect_long_ddp(buf(payload_off, payload_len), ddp_tree)
        pinfo.cols.info = string.format("DDP long  %s", tail)
    else
        pinfo.cols.info = string.format("LLAP %s  node %d->%s", type_name, src_node, dst_label)
        if payload_len > 0 then
            subtree:add(f_ddp_data, buf(payload_off, payload_len))
        end
    end

    return buf:len()
end

-- Register on the LToUDP UDP port. If your capture uses a different port, add it
-- here (or via Decode As -> UDP port -> LToUDP).
local udp_table = DissectorTable.get("udp.port")
udp_table:add(LTOUDP_PORT, ltoudp)
