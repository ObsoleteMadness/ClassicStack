-- MacIPX (IPX over AppleTalk) Wireshark dissector.
--
-- Wire format reference:
--   core/protocol/macipx/macipx.go   (DDP type 0x4E framing + opcodes)
--   core/service/ipxgw/ipxgw.go      (the AppleTalk-side gateway)
--   spec/15-macipx-gateway.md        (observed / golden wire format)
--   captures/nw41-macipxgw.pcapng    (golden: real NetWare 4.1 MACIPXGW session)
--
-- Mac OS MacIPX tunnels Novell IPX inside AppleTalk DDP datagrams of TYPE 78
-- (0x4E) on socket 78. The first payload byte is an OPCODE; what follows depends
-- on it (spec/15):
--
--   0x00  Data      remainder is a complete 30-byte IPX header + IPX payload
--   0x10  Listen    one or more 8-byte (node 6B, socket 2B) broadcast-listen pairs
--   0x20  RegReq    6-byte address-assignment request blob
--   0x23  RegRsp    6-byte request blob echo + assigned IPX node low 3 bytes
--                   (the full node is 7A:00:00 || those 3 bytes)
--
-- Wireshark's AppleTalk (DDP) dissector does not know type 78, so it stops at
-- "Protocol type: Unknown (78)" and the inner IPX (and its NCP/SAP/RIP payload)
-- is never decoded. This plugin picks up there: it detects DDP type 78, decodes
-- the MacIPX opcode, and for a Data frame (0x00) HANDS THE ENCAPSULATED IPX TO
-- WIRESHARK'S BUILT-IN `ipx` DISSECTOR — so the full native IPX → RIP/SAP/NCP/
-- NBIPX dissection appears exactly as for any raw IPX frame. The control opcodes
-- (Listen / RegReq / RegRsp) are decoded inline into their own subtree.
--
-- Like mactcp.lua it is a POSTDISSECTOR, not a DDP sub-dissector: the AppleTalk
-- dissector exposes no `ddp.type` dissector table to register on. The
-- postdissector runs after DDP, reads `ddp.type` to detect 78, and locates the
-- opcode byte at the payload immediately after the `ddp.type` header byte (its
-- absolute frame offset) — which works across every DDP carrier (LToUDP,
-- EtherTalk, TashTalk) without hard-coding per-encapsulation offsets.
--
-- Install with: Wireshark -> Help -> About Wireshark -> Folders -> Personal Lua
-- Plugins, copy this file there (alongside mactcp.lua / ltoudp.lua), then
-- Analyze -> Reload Lua Plugins. No manual "Decode As" is needed — it fires
-- automatically.

local macipx = Proto("macipx", "MacIPX (IPX over AppleTalk)")

-- MacIPX rides DDP type 0x4E (78) on socket 78; the socket check guards against a
-- stray type-78 datagram on some other socket.
local MACIPX_DDP_TYPE = 78
local MACIPX_SOCKET = 78

-- Opcodes (spec/15 §Sub-protocol).
local OP_DATA = 0x00
local OP_LISTEN = 0x10
local OP_REGREQ = 0x20
local OP_REGRSP = 0x23

local opcode_names = {
    [OP_DATA] = "Data (encapsulated IPX)",
    [OP_LISTEN] = "Listen (register broadcast sockets)",
    [OP_REGREQ] = "Register Request",
    [OP_REGRSP] = "Register Reply",
}

-- The 3-byte prefix every MacIPX-assigned IPX node carries (spec/15). The reply
-- delivers only the low 3 bytes; the full node is this prefix || those 3 bytes.
local NODE_PREFIX = "7a:00:00:"

-- The built-in IPX dissector we hand the decapsulated Data payload to.
local ipx_dissector = Dissector.get("ipx")

-- Field accessors for values DDP has already dissected this frame.
local f_ddp_type = Field.new("ddp.type")
local f_ddp_dstsock = Field.new("ddp.dst_socket")
local f_ddp_srcsock = Field.new("ddp.src_socket")

local pf_opcode = ProtoField.uint8("macipx.opcode", "Opcode", base.HEX, opcode_names)
local pf_reg_blob = ProtoField.bytes("macipx.reg.blob", "Request blob")
local pf_reg_node = ProtoField.string("macipx.reg.node", "Assigned IPX node")
local pf_listen_node = ProtoField.ether("macipx.listen.node", "Listen node")
local pf_listen_sock = ProtoField.uint16("macipx.listen.socket", "Listen IPX socket", base.HEX)
macipx.fields = { pf_opcode, pf_reg_blob, pf_reg_node, pf_listen_node, pf_listen_sock }

-- matchesMacIPX reports whether this frame is a MacIPX datagram (DDP type 78 on
-- socket 78 either way), returning the FieldInfo for ddp.type so the caller can
-- locate the opcode/payload.
local function matchesMacIPX()
    local t = f_ddp_type()
    if not t or t.value ~= MACIPX_DDP_TYPE then
        return nil
    end
    local ds, ss = f_ddp_dstsock(), f_ddp_srcsock()
    local okSock = (ds and ds.value == MACIPX_SOCKET) or (ss and ss.value == MACIPX_SOCKET)
    if not okSock then
        return nil
    end
    return t
end

-- hexNode renders the 3 assigned low bytes as the full 7a:00:00:xx:xx:xx node.
local function fullNode(tvb, off)
    return string.format("%s%02x:%02x:%02x", NODE_PREFIX,
        tvb(off, 1):uint(), tvb(off + 1, 1):uint(), tvb(off + 2, 1):uint())
end

function macipx.dissector(tvb, pinfo, tree)
    local t = matchesMacIPX()
    if not t then
        return
    end

    -- The DDP payload (the MacIPX opcode) begins immediately after the ddp.type
    -- byte. FieldInfo offsets are absolute within the frame tvb, so this is
    -- correct regardless of the DDP header form or the outer carrier.
    local off = t.offset + t.len
    local frameLen = tvb:len()
    if off >= frameLen then
        return
    end

    local opcode = tvb(off, 1):uint()
    local sub = tree:add(macipx, tvb(off, frameLen - off),
        "MacIPX (DDP type 78, socket 78)")
    sub:add(pf_opcode, tvb(off, 1))
    local rest = off + 1

    if opcode == OP_DATA then
        pinfo.cols.protocol:set("MacIPX")
        if rest >= frameLen then
            return
        end
        -- Hand the encapsulated IPX to Wireshark's built-in dissector, which
        -- takes over from IPX down (RIP/SAP/NCP/NBIPX…).
        local ipx_tvb = tvb(rest):tvb()
        ipx_dissector:call(ipx_tvb, pinfo, tree)
        return
    end

    pinfo.cols.protocol:set("MacIPX")
    pinfo.cols.info:set("MacIPX " .. (opcode_names[opcode] or string.format("op 0x%02x", opcode)))

    if opcode == OP_REGREQ then
        if rest + 6 <= frameLen then
            sub:add(pf_reg_blob, tvb(rest, 6))
        end
    elseif opcode == OP_REGRSP then
        -- 6-byte blob echo + 3 assigned low bytes.
        if rest + 6 <= frameLen then
            sub:add(pf_reg_blob, tvb(rest, 6))
        end
        if rest + 9 <= frameLen then
            sub:add(pf_reg_node, tvb(rest + 6, 3), fullNode(tvb, rest + 6))
        end
    elseif opcode == OP_LISTEN then
        -- One or more 8-byte (node 6B, socket 2B) pairs.
        local p = rest
        while p + 8 <= frameLen do
            local pair = sub:add(macipx, tvb(p, 8), "Listen entry")
            pair:add(pf_listen_node, tvb(p, 6))
            pair:add(pf_listen_sock, tvb(p + 6, 2))
            p = p + 8
        end
    end
end

-- Register as a postdissector: it runs on every frame, after DDP dissection, and
-- self-selects via matchesMacIPX().
register_postdissector(macipx)
