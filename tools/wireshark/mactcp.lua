-- MacTCP / MacIP Wireshark dissector.
--
-- Wire format reference:
--   core/service/macip/macip.go     (DDP type 22 = MacIP IP data on socket 72)
--   core/service/macip/tcp.go       (the DDP payload IS a raw IPv4 packet)
--   spec/14-macip-gateway.md §5     ("There is no MacIP header on data packets —
--                                     the DDP payload is the IP packet")
--
-- MacIP ("IPTalk" / KIP) tunnels a Mac's IPv4 traffic inside AppleTalk DDP
-- datagrams of TYPE 22 on socket 72. The DDP data field is a verbatim IPv4
-- packet (header + payload), starting at the IPv4 version/IHL byte — there is no
-- intervening MacIP header on data packets (address assignment uses ATP / DDP
-- type 3 instead, which this dissector leaves alone).
--
-- Wireshark's AppleTalk (DDP) dissector does not know type 22, so it stops at
-- "Protocol type: Unknown (22)" and the inner IP/TCP/UDP is never decoded. This
-- plugin picks up there: it detects DDP type 22, then HANDS THE PAYLOAD TO
-- WIRESHARK'S BUILT-IN `ip` DISSECTOR, so the full native IPv4 → TCP/UDP → HTTP…
-- dissection (including checksum validation, which is how the SYN-ACK checksum
-- bug was caught) appears exactly as for any other IP packet.
--
-- It is implemented as a POSTDISSECTOR rather than a DDP sub-dissector because
-- the AppleTalk dissector exposes no `ddp.type` dissector table to register on.
-- A postdissector runs after DDP has dissected, reads the `ddp.type` field to
-- detect 22, and locates the payload immediately after the `ddp.type` header
-- byte (its absolute frame offset) — which works across every DDP carrier
-- (LToUDP, EtherTalk, TashTalk) without hard-coding per-encapsulation offsets.
--
-- Install with: Wireshark -> Help -> About Wireshark -> Folders -> Personal Lua
-- Plugins, copy this file there (alongside ltoudp.lua), then Analyze -> Reload
-- Lua Plugins. No manual "Decode As" is needed — it fires automatically.

local mactcp = Proto("mactcp", "MacTCP (IP over AppleTalk / MacIP)")

-- MacIP data rides DDP type 22 (0x16) on socket 72; the socket check guards
-- against a stray type-22 datagram on some other socket.
local MACIP_DDP_TYPE = 22
local MACIP_SOCKET = 72

-- The built-in IPv4 dissector we hand the decapsulated payload to.
local ip_dissector = Dissector.get("ip")

-- Field accessors for values DDP has already dissected this frame.
local f_ddp_type = Field.new("ddp.type")
local f_ddp_dstsock = Field.new("ddp.dst_socket")
local f_ddp_srcsock = Field.new("ddp.src_socket")

local pf_info = ProtoField.string("mactcp.info", "MacIP")
mactcp.fields = { pf_info }

-- matchesMacIP reports whether this frame is a MacIP data datagram (DDP type 22
-- on socket 72 either way), returning the FieldInfo for ddp.type so the caller
-- can locate the payload.
local function matchesMacIP()
    local t = f_ddp_type()
    if not t or t.value ~= MACIP_DDP_TYPE then
        return nil
    end
    -- Socket 72 on either end (config uses 72↔72; data likewise). Accept when
    -- either socket is 72 so we still decode if one end is unusual.
    local ds, ss = f_ddp_dstsock(), f_ddp_srcsock()
    local okSock = (ds and ds.value == MACIP_SOCKET) or (ss and ss.value == MACIP_SOCKET)
    if not okSock then
        return nil
    end
    return t
end

function mactcp.dissector(tvb, pinfo, tree)
    local t = matchesMacIP()
    if not t then
        return
    end

    -- The DDP payload (the IPv4 packet) begins immediately after the ddp.type
    -- byte. FieldInfo offsets are absolute within the frame tvb, so this is
    -- correct regardless of the DDP header form or the outer carrier.
    local payloadOffset = t.offset + t.len
    local frameLen = tvb:len()
    if payloadOffset >= frameLen then
        return
    end

    -- Sanity-check the first byte looks like IPv4 (version nibble 4) before
    -- handing off, so a mis-detected frame does not spam the IP dissector.
    local first = tvb(payloadOffset, 1):uint()
    if bit.rshift(first, 4) ~= 4 then
        return
    end

    tree:add(mactcp, tvb(payloadOffset, frameLen - payloadOffset),
        "MacTCP: IPv4 over AppleTalk (DDP type 22, socket 72)")

    -- Hand the decapsulated IPv4 packet to Wireshark's built-in dissector, which
    -- takes over from IP down (TCP/UDP/ICMP/HTTP…), with full checksum checks.
    local ip_tvb = tvb(payloadOffset):tvb()
    ip_dissector:call(ip_tvb, pinfo, tree)

    -- Mark the protocol column so MacIP frames are easy to filter/spot.
    pinfo.cols.protocol:set("MacTCP")
end

-- Register as a postdissector: it runs on every frame, after DDP dissection, and
-- self-selects via matchesMacIP().
register_postdissector(mactcp)
