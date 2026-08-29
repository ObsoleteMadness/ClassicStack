-- EtherDFS ("The Ethernet DOS File System", by Mateusz Viste) Wireshark dissector.
--
-- Wire format reference: core/protocol/etherdfs/{frame,opcodes,requests,replies}.go
-- and spec/etherdfs.txt / spec/errata.md in this repo.
--
-- EtherDFS rides directly on Ethernet II with EtherType 0xEDF5 (no 802.2/SNAP).
-- Frame layout (offsets from the start of the Ethernet payload, i.e. after the
-- 14-byte Ethernet header, or absolute frame offsets when noted):
--
--   offset 0-13   Ethernet header (dst MAC, src MAC, EtherType)
--   offset 14-51  38 bytes of padding (no protocol meaning; pads short frames
--                 up to the 46-byte Ethernet minimum payload)
--   offset 52-53  Size:      2 bytes LE, total frame length (0 = "use Ethernet length")
--   offset 54-55  Checksum:  2 bytes LE, BSD checksum over [56:] (only if CKS flag set)
--   offset 56     Version:   low 7 bits = protocol version (2), high bit = CKS flag
--   offset 57     Sequence:  1 byte, client request sequence number (echoed in reply)
--   offset 58     Drive/StatusLo: request = drive number (low 5 bits); reply = AX status low byte
--   offset 59     Opcode/StatusHi: request = AL_* opcode; reply = AX status high byte
--   offset 60+    Payload: per-opcode body
--
-- Install with: Wireshark -> Help -> About Wireshark -> Folders -> Personal Lua
-- Plugins, then copy this file there (or symlink it), and Analyze -> Reload Lua
-- Plugins.

local etherdfs = Proto("etherdfs", "EtherDFS")

-- ---------------------------------------------------------------------------
-- AL_* opcodes (core/protocol/etherdfs/opcodes.go)
-- ---------------------------------------------------------------------------
local opcodes = {
    [0x00] = "AL_INSTALLCHK",
    [0x01] = "AL_RMDIR",
    [0x03] = "AL_MKDIR",
    [0x05] = "AL_CHDIR",
    [0x06] = "AL_CLSFIL",
    [0x07] = "AL_CMMTFIL",
    [0x08] = "AL_READFIL",
    [0x09] = "AL_WRITEFIL",
    [0x0A] = "AL_LOCKFIL",
    [0x0B] = "AL_UNLOCKFIL",
    [0x0C] = "AL_DISKSPACE",
    [0x0E] = "AL_SETATTR",
    [0x0F] = "AL_GETATTR",
    [0x11] = "AL_RENAME",
    [0x13] = "AL_DELETE",
    [0x16] = "AL_OPEN",
    [0x17] = "AL_CREATE",
    [0x1B] = "AL_FINDFIRST",
    [0x1C] = "AL_FINDNEXT",
    [0x21] = "AL_SKFMEND",
    [0x2E] = "AL_SPOPNFIL",
}

-- DOS error codes (core/protocol/etherdfs/opcodes.go)
local errors_ = {
    [0x00] = "Success",
    [0x02] = "File not found",
    [0x03] = "Path not found",
    [0x05] = "Access denied",
    [0x06] = "Invalid file handle",
    [0x12] = "No more files",
    [0x1D] = "Write fault",
    [0x1E] = "Read fault",
    [0x50] = "File already exists",
}

local ETHERDFS_ETHERTYPE = 0xEDF5
local PROTOCOL_VERSION_MASK = 0x7F
local CKS_FLAG = 0x80
local HEADER_END = 60 -- absolute frame offset where the payload begins

-- ---------------------------------------------------------------------------
-- Fields
-- ---------------------------------------------------------------------------
local f_size      = ProtoField.uint16("etherdfs.size", "Size", base.DEC)
local f_checksum  = ProtoField.uint16("etherdfs.checksum", "Checksum", base.HEX)
local f_version   = ProtoField.uint8("etherdfs.version", "Version", base.DEC, nil, PROTOCOL_VERSION_MASK)
local f_cks_flag  = ProtoField.bool("etherdfs.cks", "Checksum present", 8, nil, CKS_FLAG)
local f_sequence  = ProtoField.uint8("etherdfs.sequence", "Sequence", base.DEC)
local f_drive     = ProtoField.uint8("etherdfs.drive", "Drive", base.DEC, nil, 0x1F)
local f_opcode    = ProtoField.uint8("etherdfs.opcode", "Opcode", base.HEX, opcodes)
local f_status    = ProtoField.uint16("etherdfs.status", "Status (AX)", base.HEX, errors_)
local f_isreply   = ProtoField.bool("etherdfs.isreply", "Is reply")
local f_payload   = ProtoField.bytes("etherdfs.payload", "Payload")

-- Request body fields
local f_req_offset   = ProtoField.uint32("etherdfs.req.offset", "Offset", base.DEC)
local f_req_offset32 = ProtoField.int32("etherdfs.req.offset_signed", "Offset (signed)", base.DEC)
local f_req_fileid   = ProtoField.uint16("etherdfs.req.fileid", "File ID", base.HEX)
local f_req_length   = ProtoField.uint16("etherdfs.req.length", "Length", base.DEC)
local f_req_data     = ProtoField.bytes("etherdfs.req.data", "Data")
local f_req_attr_ss  = ProtoField.uint16("etherdfs.req.attr", "Attr (SS)", base.HEX)
local f_req_action   = ProtoField.uint16("etherdfs.req.action", "Action (CC)", base.HEX)
local f_req_openmode = ProtoField.uint16("etherdfs.req.openmode", "OpenMode (MM)", base.HEX)
local f_req_path     = ProtoField.string("etherdfs.req.path", "Path")
local f_req_attrfilter = ProtoField.uint8("etherdfs.req.attrfilter", "Attribute filter", base.HEX)
local f_req_dirid    = ProtoField.uint16("etherdfs.req.dirid", "Directory ID", base.HEX)
local f_req_position = ProtoField.uint16("etherdfs.req.position", "Position", base.DEC)
local f_req_mask     = ProtoField.string("etherdfs.req.mask", "FCB search mask")
local f_req_attr8    = ProtoField.uint8("etherdfs.req.attr8", "Attribute", base.HEX)
local f_req_srclen   = ProtoField.uint8("etherdfs.req.srclen", "Source length", base.DEC)
local f_req_src      = ProtoField.string("etherdfs.req.src", "Source path")
local f_req_dst      = ProtoField.string("etherdfs.req.dst", "Destination path")

-- Reply body fields
local f_rep_totalclusters = ProtoField.uint16("etherdfs.rep.totalclusters", "Total 32KB clusters", base.DEC)
local f_rep_bytespersector = ProtoField.uint16("etherdfs.rep.bytespersector", "Bytes per sector", base.DEC)
local f_rep_freeclusters = ProtoField.uint16("etherdfs.rep.freeclusters", "Free 32KB clusters", base.DEC)
local f_rep_time     = ProtoField.uint32("etherdfs.rep.time", "DOS date/time", base.HEX)
local f_rep_size     = ProtoField.uint32("etherdfs.rep.size", "File size", base.DEC)
local f_rep_attr     = ProtoField.uint8("etherdfs.rep.attr", "Attribute", base.HEX)
local f_rep_fcb      = ProtoField.string("etherdfs.rep.fcb", "FCB name (8.3)")
local f_rep_dirid    = ProtoField.uint16("etherdfs.rep.dirid", "Directory ID", base.HEX)
local f_rep_position = ProtoField.uint16("etherdfs.rep.position", "Position", base.DEC)
local f_rep_fileid   = ProtoField.uint16("etherdfs.rep.fileid", "File ID", base.HEX)
local f_rep_action   = ProtoField.uint16("etherdfs.rep.action", "Action result (CX)", base.HEX)
local f_rep_mode     = ProtoField.uint8("etherdfs.rep.mode", "Open mode", base.HEX)
local f_rep_written  = ProtoField.uint16("etherdfs.rep.written", "Bytes written", base.DEC)
local f_rep_newoffset = ProtoField.uint32("etherdfs.rep.newoffset", "New offset", base.DEC)
local f_rep_data     = ProtoField.bytes("etherdfs.rep.data", "Data")

etherdfs.fields = {
    f_size, f_checksum, f_version, f_cks_flag, f_sequence, f_drive, f_opcode,
    f_status, f_isreply, f_payload,
    f_req_offset, f_req_offset32, f_req_fileid, f_req_length, f_req_data,
    f_req_attr_ss, f_req_action, f_req_openmode, f_req_path,
    f_req_attrfilter, f_req_dirid, f_req_position, f_req_mask, f_req_attr8,
    f_req_srclen, f_req_src, f_req_dst,
    f_rep_totalclusters, f_rep_bytespersector, f_rep_freeclusters,
    f_rep_time, f_rep_size, f_rep_attr, f_rep_fcb, f_rep_dirid, f_rep_position,
    f_rep_fileid, f_rep_action, f_rep_mode, f_rep_written, f_rep_newoffset,
    f_rep_data,
}

-- ---------------------------------------------------------------------------
-- FCB (8.3) decode helper: 11 bytes, 8 base + 3 ext, space padded.
-- ---------------------------------------------------------------------------
local function fcb_to_name(buf)
    local raw = buf:raw()
    local base_ = raw:sub(1, 8):gsub("%s+$", "")
    local ext = raw:sub(9, 11):gsub("%s+$", "")
    if ext == "" then
        return base_
    end
    return base_ .. "." .. ext
end

-- ---------------------------------------------------------------------------
-- Per-opcode request body dissection. buf is the payload tvb range.
-- ---------------------------------------------------------------------------
local function dissect_request_body(opcode, buf, tree)
    local len = buf:len()
    if len == 0 then
        return
    end

    if opcode == 0x08 then -- AL_READFIL
        if len < 8 then return end
        tree:add_le(f_req_offset, buf(0, 4))
        tree:add_le(f_req_fileid, buf(4, 2))
        tree:add_le(f_req_length, buf(6, 2))
    elseif opcode == 0x09 then -- AL_WRITEFIL
        if len < 6 then return end
        tree:add_le(f_req_offset, buf(0, 4))
        tree:add_le(f_req_fileid, buf(4, 2))
        if len > 6 then
            tree:add(f_req_data, buf(6, len - 6))
        end
    elseif opcode == 0x21 then -- AL_SKFMEND
        if len < 6 then return end
        tree:add_le(f_req_offset32, buf(0, 4))
        tree:add_le(f_req_fileid, buf(4, 2))
    elseif opcode == 0x16 or opcode == 0x17 or opcode == 0x2E then
        -- AL_OPEN / AL_CREATE / AL_SPOPNFIL: SS,CC,MM then path (always all 3 words).
        if len < 6 then return end
        tree:add_le(f_req_attr_ss, buf(0, 2))
        tree:add_le(f_req_action, buf(2, 2))
        tree:add_le(f_req_openmode, buf(4, 2))
        if len > 6 then
            tree:add(f_req_path, buf(6, len - 6))
        end
    elseif opcode == 0x1B then -- AL_FINDFIRST
        tree:add(f_req_attrfilter, buf(0, 1))
        if len > 1 then
            tree:add(f_req_path, buf(1, len - 1))
        end
    elseif opcode == 0x1C then -- AL_FINDNEXT
        if len < 5 + 11 then return end
        tree:add_le(f_req_dirid, buf(0, 2))
        tree:add_le(f_req_position, buf(2, 2))
        tree:add(f_req_attrfilter, buf(4, 1))
        tree:add(f_req_mask, buf(5, 11), fcb_to_name(buf(5, 11)))
    elseif opcode == 0x0E then -- AL_SETATTR
        if len < 1 then return end
        tree:add(f_req_attr8, buf(0, 1))
        if len > 1 then
            tree:add(f_req_path, buf(1, len - 1))
        end
    elseif opcode == 0x11 then -- AL_RENAME
        if len < 1 then return end
        local srclen = buf(0, 1):uint()
        tree:add(f_req_srclen, buf(0, 1))
        if len < 1 + srclen then return end
        tree:add(f_req_src, buf(1, srclen))
        if len > 1 + srclen then
            tree:add(f_req_dst, buf(1 + srclen, len - 1 - srclen))
        end
    elseif opcode == 0x01 or opcode == 0x03 or opcode == 0x05
        or opcode == 0x13 or opcode == 0x0F then
        -- AL_RMDIR / AL_MKDIR / AL_CHDIR / AL_DELETE / AL_GETATTR: bare path.
        tree:add(f_req_path, buf(0, len))
    else
        tree:add(f_payload, buf(0, len))
    end
end

-- ---------------------------------------------------------------------------
-- Per-opcode reply body dissection. Status (AX) is in the header, not here,
-- except AL_DISKSPACE where the header word is content (media/sectors-per-
-- cluster), not a status - see core/protocol/etherdfs/replies.go.
-- ---------------------------------------------------------------------------
local function dissect_reply_body(opcode, buf, tree)
    local len = buf:len()
    if len == 0 then
        return
    end

    if opcode == 0x0C then -- AL_DISKSPACE: BX, CX, DX (6 bytes)
        if len < 6 then return end
        tree:add_le(f_rep_totalclusters, buf(0, 2))
        tree:add_le(f_rep_bytespersector, buf(2, 2))
        tree:add_le(f_rep_freeclusters, buf(4, 2))
    elseif opcode == 0x0F then -- AL_GETATTR: time, size, attr (9 bytes)
        if len < 9 then return end
        tree:add_le(f_rep_time, buf(0, 4))
        tree:add_le(f_rep_size, buf(4, 4))
        tree:add(f_rep_attr, buf(8, 1))
    elseif opcode == 0x1B or opcode == 0x1C then -- AL_FINDFIRST / AL_FINDNEXT
        if len < 1 + 11 + 12 then return end
        tree:add(f_rep_attr, buf(0, 1))
        tree:add(f_rep_fcb, buf(1, 11), fcb_to_name(buf(1, 11)))
        local o = 1 + 11
        tree:add_le(f_rep_time, buf(o, 4))
        tree:add_le(f_rep_size, buf(o + 4, 4))
        tree:add_le(f_rep_dirid, buf(o + 8, 2))
        tree:add_le(f_rep_position, buf(o + 10, 2))
    elseif opcode == 0x16 or opcode == 0x17 or opcode == 0x2E then
        -- AL_OPEN / AL_CREATE / AL_SPOPNFIL: always 25 bytes.
        if len < 25 then return end
        tree:add(f_rep_attr, buf(0, 1))
        tree:add(f_rep_fcb, buf(1, 11), fcb_to_name(buf(1, 11)))
        local o = 1 + 11
        tree:add_le(f_rep_time, buf(o, 4))
        tree:add_le(f_rep_size, buf(o + 4, 4))
        tree:add_le(f_rep_fileid, buf(o + 8, 2))
        tree:add_le(f_rep_action, buf(o + 10, 2))
        tree:add(f_rep_mode, buf(o + 12, 1))
    elseif opcode == 0x08 then -- AL_READFIL: raw data
        tree:add(f_rep_data, buf(0, len))
    elseif opcode == 0x09 then -- AL_WRITEFIL: bytes-written count
        if len < 2 then return end
        tree:add_le(f_rep_written, buf(0, 2))
    elseif opcode == 0x21 then -- AL_SKFMEND: new absolute offset
        if len < 4 then return end
        tree:add_le(f_rep_newoffset, buf(0, 4))
    else
        tree:add(f_payload, buf(0, len))
    end
end

-- ---------------------------------------------------------------------------
-- Request/reply direction and per-sequence opcode tracking.
--
-- EtherDFS has no explicit request/reply flag on the wire (header offset
-- 58-59 means Drive+Opcode for a request but the AX Status word for a reply -
-- see Frame.Reply/Encode in core/protocol/etherdfs/frame.go). The client TSR
-- and server both know which they are; a passive observer has to infer it
-- from direction instead. Since EtherDFS is a strict one-client-per-server-
-- reply exchange, the first frame from a given source MAC that decodes to a
-- plausible request (opcode/drive shape) marks that MAC as "the client" for
-- the rest of the capture; all frames from the other endpoint are replies.
-- This survives status words that happen to collide with opcode values
-- (e.g. status 0x0002 vs. AL_MKDIR's low byte), which a byte-shape-only
-- heuristic cannot.
-- ---------------------------------------------------------------------------
local client_macs = {} -- eth src (as string) -> true, once identified as a client

-- request_log[src_mac][sequence] = opcode, so the reply (from the other MAC,
-- same sequence) can be decoded with the right body shape.
local request_log = {}

-- ---------------------------------------------------------------------------
-- Main dissector. buf covers the whole Ethernet payload starting right after
-- the EtherType (Wireshark hands ethertype dissectors the payload only), so
-- our fixed offsets below are relative to that (offset 52 in the frame is
-- offset 38 here: 52 - 14 for the Ethernet header).
-- ---------------------------------------------------------------------------
function etherdfs.dissector(buf, pinfo, tree)
    if buf:len() < (HEADER_END - 14) then
        return 0
    end

    pinfo.cols.protocol = "EtherDFS"

    local off_size     = 52 - 14
    local off_checksum = 54 - 14
    local off_version  = 56 - 14
    local off_sequence = 57 - 14
    local off_drive    = 58 - 14
    local off_opcode   = 59 - 14
    local header_end   = HEADER_END - 14

    local version_byte = buf(off_version, 1):uint()
    local cks = bit.band(version_byte, CKS_FLAG) ~= 0

    local size_field = buf(off_size, 2):le_uint()
    local total_len = buf:len()
    if size_field > 0 and size_field <= total_len then
        total_len = size_field
    end

    local subtree = tree:add(etherdfs, buf(0, header_end), "EtherDFS")
    subtree:add(f_size, buf(off_size, 2), size_field)
    subtree:add(f_checksum, buf(off_checksum, 2))
    subtree:add(f_version, buf(off_version, 1))
    subtree:add(f_cks_flag, buf(off_version, 1))
    subtree:add(f_sequence, buf(off_sequence, 1))

    local opcode_or_status = buf(off_drive, 2):le_uint()
    local seq = buf(off_sequence, 1):uint()

    local src = tostring(pinfo.src)
    local dst = tostring(pinfo.dst)

    -- Classify direction: known client -> request, known server (i.e. src is
    -- the peer of a known client) -> reply. On the very first frame of a
    -- capture neither MAC is known yet; assume the sender of frame 1 is the
    -- client (EtherDFS conversations always open with a client request, e.g.
    -- AL_INSTALLCHK or the first real call).
    local is_request
    if client_macs[src] then
        is_request = true
    elseif client_macs[dst] then
        is_request = false
    else
        client_macs[src] = true
        is_request = true
    end

    if is_request then
        local drive = bit.band(buf(off_drive, 1):uint(), 0x1F)
        local opcode = buf(off_opcode, 1):uint()
        subtree:add(f_drive, buf(off_drive, 1))
        subtree:add(f_opcode, buf(off_opcode, 1))
        subtree:add(f_isreply, false):set_generated()

        local opname = opcodes[opcode] or string.format("Unknown (0x%02X)", opcode)
        pinfo.cols.info = string.format("REQUEST %s drive=%d seq=%d", opname, drive, seq)

        if total_len > header_end then
            local paytree = subtree:add(f_payload, buf(header_end, total_len - header_end))
            dissect_request_body(opcode, buf(header_end, total_len - header_end), paytree)
        end

        request_log[src] = request_log[src] or {}
        request_log[src][seq] = opcode
    else
        subtree:add(f_status, buf(off_drive, 2), opcode_or_status)
        subtree:add(f_isreply, true):set_generated()

        local errname = errors_[opcode_or_status] or string.format("0x%04X", opcode_or_status)
        pinfo.cols.info = string.format("REPLY status=%s seq=%d", errname, seq)

        if total_len > header_end then
            local paytree = subtree:add(f_payload, buf(header_end, total_len - header_end))
            -- Reply body shape isn't on the wire; look up the opcode from
            -- the matching request (same sequence number, sent by dst - the
            -- client this reply is answering).
            local log = request_log[dst]
            local opcode = log and log[seq]
            if opcode then
                dissect_reply_body(opcode, buf(header_end, total_len - header_end), paytree)
            end
        end
    end

    return buf:len()
end

-- Register on the custom EtherType.
local ethertype_table = DissectorTable.get("ethertype")
ethertype_table:add(ETHERDFS_ETHERTYPE, etherdfs)
