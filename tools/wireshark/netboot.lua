-- Netboot (Apple Boot Protocol + ChainBoot EBP) Wireshark dissector.
--
-- Wire format reference:
--   core/protocol/abp/abp.go          (ABP/EBP struct layouts, big-endian)
--   core/service/netboot/netboot.go   (server dispatch, command semantics)
--   spec/19-netboot.md                (full byte-level protocol + errata)
--
-- Old-World Mac ROMs (`.netBOOT` / `.ATBOOT`) netboot over DDP TYPE 10
-- (BOOTDDPTYPE) — Apple's own Boot Protocol (ABP), commands 1-7. The client
-- opens DDP socket 10 (hardcoded) and sends to the server address it learned
-- via NBP (`<serverNum-hex>:BootServer@*`); the SERVER's socket is whatever its
-- NBP tuple advertised (this server defaults to 10 too, but it is configurable
-- — spec/19 §Part A), so this dissector does NOT filter on socket, only on
-- ddp.type == 10.
--
-- Elliot Nunn's ChainBoot EBP extension (not Apple protocol, spec/19 §Part B)
-- rides the SAME ddp.type 10 with commands 128-131 ($80-$83): a streaming
-- read/write block protocol the chain-loaded driver (ChainLoader.a /
-- ChainDisk.a, our ROM-independent loader) switches to after the initial ABP
-- download, to get around ABP's RAM-residency size ceiling. ABP and EBP
-- command spaces are disjoint, so — exactly like the Go server's
-- handlePacket — this dissector dispatches purely on the command byte and
-- never needs to know which socket a frame arrived on (the client's very
-- first chain-read is observed arriving on the plain ABP boot socket, not
-- socket+1 — spec/19).
--
-- Wireshark's AppleTalk (DDP) dissector does not know type 10, so it stops at
-- "Protocol type: Unknown (10)". This plugin picks up there and decodes every
-- ABP/EBP command inline (there is no further built-in dissector to hand off
-- to — unlike macipx.lua/mactcp.lua, the payload is not itself another
-- Wireshark-known protocol).
--
-- Like macipx.lua/mactcp.lua it is a POSTDISSECTOR, not a DDP sub-dissector:
-- the AppleTalk dissector exposes no `ddp.type` dissector table to register
-- on. The postdissector runs after DDP, reads `ddp.type` to detect 10, and
-- locates the command byte at the payload immediately after the `ddp.type`
-- header byte (its absolute frame offset) — which works across every DDP
-- carrier (LToUDP, EtherTalk, TashTalk) without hard-coding per-encapsulation
-- offsets.
--
-- Install with: Wireshark -> Help -> About Wireshark -> Folders -> Personal Lua
-- Plugins, copy this file there (alongside macipx.lua / mactcp.lua /
-- ltoudp.lua), then Analyze -> Reload Lua Plugins. No manual "Decode As" is
-- needed — it fires automatically.

local netboot = Proto("netboot", "Netboot (ABP + ChainBoot EBP)")

-- ABP/EBP both ride DDP type 10 (BOOTDDPTYPE, ATBootEqu.h).
local ABP_DDP_TYPE = 10

-- Part A — ABP commands (BootDefines.h).
local CMD_NULL                = 0
local CMD_USER_RECORD_REQUEST = 1 -- rbMapUser (workstation -> server)
local CMD_USER_RECORD_REPLY   = 2 -- rbUserReply (server -> workstation)
local CMD_BOOT_IMAGE_REQUEST  = 3 -- rbImageRequest (workstation -> server)
local CMD_BOOT_IMAGE_REPLY    = 4 -- rbImageData (server -> workstation)
local CMD_IMAGE_DONE          = 5 -- unused by the boot path
local CMD_USER_RECORD_UPDATE  = 6 -- unused
local CMD_USER_UPDATE_REPLY   = 7 -- unused

-- Part B — ChainBoot EBP commands (Elliot Nunn's extension, not Apple's).
local CMD_CHAIN_READ       = 128 -- $80 chain read request (workstation -> server)
local CMD_CHAIN_READ_DATA  = 129 -- $81 chain read data (server -> workstation)
local CMD_CHAIN_WRITE      = 130 -- $82 chain write block (workstation -> server)
local CMD_CHAIN_WRITE_ACK  = 131 -- $83 chain write ack (server -> workstation)

-- ChainLastFlag (0x80) marks the final block of an EBP write chunk in the
-- blkIndex byte (abp.ChainLastFlag).
local CHAIN_LAST_FLAG = 0x80

local cmd_names = {
    [CMD_NULL]               = "Null Command",
    [CMD_USER_RECORD_REQUEST] = "rbMapUser (User Record Request)",
    [CMD_USER_RECORD_REPLY]   = "rbUserReply (User Record Reply)",
    [CMD_BOOT_IMAGE_REQUEST]  = "rbImageRequest (Boot Image Request)",
    [CMD_BOOT_IMAGE_REPLY]    = "rbImageData (Boot Image Reply)",
    [CMD_IMAGE_DONE]          = "Image Done",
    [CMD_USER_RECORD_UPDATE]  = "User Record Update",
    [CMD_USER_UPDATE_REPLY]   = "User Update Reply",
    [CMD_CHAIN_READ]          = "ChainBoot Read Request (EBP)",
    [CMD_CHAIN_READ_DATA]     = "ChainBoot Read Data (EBP)",
    [CMD_CHAIN_WRITE]         = "ChainBoot Write Block (EBP)",
    [CMD_CHAIN_WRITE_ACK]     = "ChainBoot Write Ack (EBP)",
}

-- Field accessors for values DDP has already dissected this frame.
local f_ddp_type = Field.new("ddp.type")

-- Common header fields.
local pf_cmd     = ProtoField.uint8("netboot.cmd", "Command", base.HEX, cmd_names)
local pf_version = ProtoField.uint8("netboot.version", "Version")

-- cmd 1 - UserRecordRequest (rbMapUser).
local pf_ur_machineid = ProtoField.uint16("netboot.ur.machine_id", "Machine ID (PRAM osType)")
local pf_ur_timestamp = ProtoField.uint32("netboot.ur.timestamp", "Timestamp (TickCount)")
local pf_ur_namelen   = ProtoField.uint8("netboot.ur.name_len", "User name length")
local pf_ur_name      = ProtoField.string("netboot.ur.name", "User name")

-- cmd 2 - BootPktRply (rbUserReply), 586 bytes.
local pf_br_osid       = ProtoField.uint16("netboot.reply.os_id", "OS ID (must be 1 = MACHINE_MAC)")
local pf_br_userdata   = ProtoField.uint32("netboot.reply.user_data", "User data (echoed request timestamp)")
local pf_br_blocksize  = ProtoField.uint16("netboot.reply.block_size", "Block size")
local pf_br_imageid    = ProtoField.uint16("netboot.reply.image_id", "Image ID")
local pf_br_result     = ProtoField.int16("netboot.reply.result", "Result (0 = success)")
local pf_br_imagesize  = ProtoField.uint32("netboot.reply.image_size", "Image size (blocks)")
local pf_br_userrecord = ProtoField.bytes("netboot.reply.user_record", "User record (568 bytes, typically zero)")

-- cmd 3 - BootImageRequest (rbImageRequest / bir).
local pf_ir_imageid    = ProtoField.uint16("netboot.request.image_id", "Image ID")
local pf_ir_section    = ProtoField.uint8("netboot.request.section", "Section (always 0)")
local pf_ir_flags      = ProtoField.uint8("netboot.request.flags", "Flags", base.HEX)
local pf_ir_replydelay = ProtoField.uint16("netboot.request.reply_delay", "Reply delay")
local pf_ir_bitmap     = ProtoField.bytes("netboot.request.bitmap", "Wanted-block bitmap (LSB-first; empty = flood, spec/19 errata)")

-- cmd 4 - BootBlock (rbImageData).
local pf_bb_imageid = ProtoField.uint16("netboot.block.image_id", "Image ID")
local pf_bb_blockno = ProtoField.uint16("netboot.block.block_no", "Block number (0-based, spec/19 errata)")
local pf_bb_data    = ProtoField.bytes("netboot.block.data", "Block data")

-- cmd 128 - ChainReadRequest (EBP).
local pf_cr_flag   = ProtoField.uint8("netboot.chainread.flag", "Flag (unused)", base.HEX)
local pf_cr_seq    = ProtoField.uint16("netboot.chainread.seq", "Sequence")
local pf_cr_diag   = ProtoField.uint32("netboot.chainread.image_num", "Image num (always 0, or patched-client diag counters)", base.HEX)
local pf_cr_offset = ProtoField.uint32("netboot.chainread.block_offset", "Block offset (512-byte blocks)")
local pf_cr_count  = ProtoField.uint32("netboot.chainread.block_count", "Block count (server clamps to 32)")

-- cmd 129 - ChainReadData (EBP).
local pf_cd_blkindex = ProtoField.uint8("netboot.chaindata.blk_index", "Block index (plain, no last-block flag on reads)")
local pf_cd_seq      = ProtoField.uint16("netboot.chaindata.seq", "Sequence (echoed)")
local pf_cd_data     = ProtoField.bytes("netboot.chaindata.data", "Data (must be exactly 512 bytes)")

-- cmd 130 - ChainWriteBlock (EBP).
local pf_cw_blkindex  = ProtoField.uint8("netboot.chainwrite.blk_index_raw", "Block index byte (bit7 = last block of chunk)", base.HEX)
local pf_cw_seq       = ProtoField.uint16("netboot.chainwrite.seq", "Sequence")
local pf_cw_diag      = ProtoField.uint32("netboot.chainwrite.image_num", "Image num (always 0, or patched-client raw ioPosOffset diag)", base.HEX)
local pf_cw_hunkstart = ProtoField.uint32("netboot.chainwrite.hunk_start", "Hunk start (first block of this chunk)")
local pf_cw_data      = ProtoField.bytes("netboot.chainwrite.data", "Data (<= 512 bytes)")

-- cmd 131 - ChainWriteAck (EBP).
local pf_ca_seq = ProtoField.uint16("netboot.chainack.seq", "Sequence (echoed)")

netboot.fields = {
    pf_cmd, pf_version,
    pf_ur_machineid, pf_ur_timestamp, pf_ur_namelen, pf_ur_name,
    pf_br_osid, pf_br_userdata, pf_br_blocksize, pf_br_imageid, pf_br_result, pf_br_imagesize, pf_br_userrecord,
    pf_ir_imageid, pf_ir_section, pf_ir_flags, pf_ir_replydelay, pf_ir_bitmap,
    pf_bb_imageid, pf_bb_blockno, pf_bb_data,
    pf_cr_flag, pf_cr_seq, pf_cr_diag, pf_cr_offset, pf_cr_count,
    pf_cd_blkindex, pf_cd_seq, pf_cd_data,
    pf_cw_blkindex, pf_cw_seq, pf_cw_diag, pf_cw_hunkstart, pf_cw_data,
    pf_ca_seq,
}

-- matchesNetboot reports whether this frame carries ABP/EBP (DDP type 10),
-- returning the FieldInfo for ddp.type so the caller can locate the command
-- byte. Unlike macipx/mactcp there is no socket check: the server's ABP
-- socket is NBP-advertised and configurable (spec/19), and EBP's first
-- contact is observed on that same socket rather than a fixed +1 (spec/19
-- §"130 — chain read request").
local function matchesNetboot()
    local t = f_ddp_type()
    if not t or t.value ~= ABP_DDP_TYPE then
        return nil
    end
    return t
end

-- pstr reads a MacRoman byte string as UTF-8-ish Lua text (best-effort; no
-- MacRoman table here, just strips high-bit noise for display).
local function bytesToText(tvb, off, len)
    if len <= 0 then
        return ""
    end
    return tvb(off, len):string()
end

function netboot.dissector(tvb, pinfo, tree)
    local t = matchesNetboot()
    if not t then
        return
    end

    -- The DDP payload (the ABP/EBP command byte) begins immediately after the
    -- ddp.type byte. FieldInfo offsets are absolute within the frame tvb, so
    -- this is correct regardless of the DDP header form or the outer carrier.
    local off = t.offset + t.len
    local frameLen = tvb:len()
    if off >= frameLen then
        return
    end

    local cmd = tvb(off, 1):uint()
    pinfo.cols.protocol:set("Netboot")

    local sub = tree:add(netboot, tvb(off, frameLen - off),
        "Netboot (DDP type 10): " .. (cmd_names[cmd] or string.format("unknown command 0x%02x", cmd)))
    sub:add(pf_cmd, tvb(off, 1))

    if cmd == CMD_USER_RECORD_REQUEST then
        -- 42-byte fixed record: cmd,ver,machineID(2),timestamp(4),name[34].
        if off + 42 > frameLen then return end
        sub:add(pf_version, tvb(off + 1, 1))
        sub:add(pf_ur_machineid, tvb(off + 2, 2))
        sub:add(pf_ur_timestamp, tvb(off + 4, 4))
        local namelen = tvb(off + 8, 1):uint()
        sub:add(pf_ur_namelen, tvb(off + 8, 1))
        local n = math.min(namelen, 33)
        if n > 0 then
            sub:add(pf_ur_name, tvb(off + 9, n))
        end
        pinfo.cols.info:set(string.format("Netboot rbMapUser user=\"%s\" machineID=%d",
            bytesToText(tvb, off + 9, n), tvb(off + 2, 2):uint()))

    elseif cmd == CMD_USER_RECORD_REPLY then
        -- 586-byte fixed record (DDPMaxData): cmd,ver,osid(2),userdata(4),
        -- blocksize(2),imageid(2),result(2),imagesize(4),userrecord[568].
        if off + 18 > frameLen then return end
        sub:add(pf_version, tvb(off + 1, 1))
        sub:add(pf_br_osid, tvb(off + 2, 2))
        sub:add(pf_br_userdata, tvb(off + 4, 4))
        sub:add(pf_br_blocksize, tvb(off + 8, 2))
        sub:add(pf_br_imageid, tvb(off + 10, 2))
        sub:add(pf_br_result, tvb(off + 12, 2))
        sub:add(pf_br_imagesize, tvb(off + 14, 4))
        if off + 18 + 568 <= frameLen then
            sub:add(pf_br_userrecord, tvb(off + 18, 568))
        end
        pinfo.cols.info:set(string.format("Netboot rbUserReply blocks=%d blockSize=%d result=%d",
            tvb(off + 14, 4):uint(), tvb(off + 8, 2):uint(), tvb(off + 12, 2):int()))

    elseif cmd == CMD_BOOT_IMAGE_REQUEST then
        -- 8-byte header + variable bitmap: cmd,ver,imageid(2),section,flags,replydelay(2),bitmap.
        if off + 8 > frameLen then return end
        sub:add(pf_version, tvb(off + 1, 1))
        sub:add(pf_ir_imageid, tvb(off + 2, 2))
        sub:add(pf_ir_section, tvb(off + 4, 1))
        sub:add(pf_ir_flags, tvb(off + 5, 1))
        sub:add(pf_ir_replydelay, tvb(off + 6, 2))
        local bmlen = frameLen - (off + 8)
        if bmlen > 0 then
            sub:add(pf_ir_bitmap, tvb(off + 8, bmlen))
        end
        pinfo.cols.info:set(string.format("Netboot rbImageRequest imageID=%d bitmapLen=%d",
            tvb(off + 2, 2):uint(), bmlen))

    elseif cmd == CMD_BOOT_IMAGE_REPLY then
        -- 6-byte header + block data: cmd,ver,imageid(2),blockno(2),data.
        if off + 6 > frameLen then return end
        sub:add(pf_version, tvb(off + 1, 1))
        sub:add(pf_bb_imageid, tvb(off + 2, 2))
        sub:add(pf_bb_blockno, tvb(off + 4, 2))
        local dlen = frameLen - (off + 6)
        if dlen > 0 then
            sub:add(pf_bb_data, tvb(off + 6, dlen))
        end
        pinfo.cols.info:set(string.format("Netboot rbImageData block=%d len=%d",
            tvb(off + 4, 2):uint(), dlen))

    elseif cmd == CMD_IMAGE_DONE or cmd == CMD_USER_RECORD_UPDATE or cmd == CMD_USER_UPDATE_REPLY then
        -- Not part of the served boot path (spec/19); decode only the common header.
        if off + 2 <= frameLen then
            sub:add(pf_version, tvb(off + 1, 1))
        end
        pinfo.cols.info:set("Netboot " .. cmd_names[cmd])

    elseif cmd == CMD_CHAIN_READ then
        -- 16-byte fixed record: cmd,flag,seq(2),imagenum(4),blockoffset(4),blockcount(4).
        if off + 16 > frameLen then return end
        sub:add(pf_cr_flag, tvb(off + 1, 1))
        sub:add(pf_cr_seq, tvb(off + 2, 2))
        sub:add(pf_cr_diag, tvb(off + 4, 4))
        sub:add(pf_cr_offset, tvb(off + 8, 4))
        sub:add(pf_cr_count, tvb(off + 12, 4))
        pinfo.cols.info:set(string.format("ChainBoot read seq=%d offset=%d count=%d",
            tvb(off + 2, 2):uint(), tvb(off + 8, 4):uint(), tvb(off + 12, 4):uint()))

    elseif cmd == CMD_CHAIN_READ_DATA then
        -- 4-byte header + up to 512 bytes data: cmd,blkindex,seq(2),data.
        if off + 4 > frameLen then return end
        sub:add(pf_cd_blkindex, tvb(off + 1, 1))
        sub:add(pf_cd_seq, tvb(off + 2, 2))
        local dlen = math.min(512, frameLen - (off + 4))
        if dlen > 0 then
            sub:add(pf_cd_data, tvb(off + 4, dlen))
        end
        pinfo.cols.info:set(string.format("ChainBoot read-data seq=%d blkIndex=%d len=%d",
            tvb(off + 2, 2):uint(), tvb(off + 1, 1):uint(), dlen))

    elseif cmd == CMD_CHAIN_WRITE then
        -- 12-byte header + up to 512 bytes data: cmd,blkindex,seq(2),imagenum(4),hunkstart(4),data.
        if off + 12 > frameLen then return end
        local rawIdx = tvb(off + 1, 1):uint()
        local last = bit.band(rawIdx, CHAIN_LAST_FLAG) ~= 0
        local idx = bit.band(rawIdx, 0x7F) % 32
        sub:add(pf_cw_blkindex, tvb(off + 1, 1))
        sub:add(netboot, tvb(off + 1, 1), string.format(
            "Decoded: index-in-chunk=%d, last block of chunk=%s", idx, tostring(last)))
        sub:add(pf_cw_seq, tvb(off + 2, 2))
        sub:add(pf_cw_diag, tvb(off + 4, 4))
        sub:add(pf_cw_hunkstart, tvb(off + 8, 4))
        local dlen = math.min(512, frameLen - (off + 12))
        if dlen > 0 then
            sub:add(pf_cw_data, tvb(off + 12, dlen))
        end
        pinfo.cols.info:set(string.format("ChainBoot write seq=%d hunkStart=%d idx=%d last=%s len=%d",
            tvb(off + 2, 2):uint(), tvb(off + 8, 4):uint(), idx, tostring(last), dlen))

    elseif cmd == CMD_CHAIN_WRITE_ACK then
        -- 4-byte fixed record: cmd,reserved,seq(2).
        if off + 4 > frameLen then return end
        sub:add(pf_ca_seq, tvb(off + 2, 2))
        pinfo.cols.info:set(string.format("ChainBoot write-ack seq=%d", tvb(off + 2, 2):uint()))

    else
        pinfo.cols.info:set(string.format("Netboot unknown command 0x%02x", cmd))
    end
end

-- Register as a postdissector: it runs on every frame, after DDP dissection,
-- and self-selects via matchesNetboot().
register_postdissector(netboot)
