# LocalTalk over UDP (LToUDP)

This document describes a simple way of embedding Apple LLAP (LocalTalk Link Access Protocol) packets inside UDP, which enables a "virtual" LocalTalk network to be created on top of an existing IP LAN.


## Why?

- To provide reasonably plug-and-play networking for Mini vMac that doesn't need root privileges, that is easily portable between OSes, and that plays nicely with having multiple instances running on one host computer.
- To write "code as documentation" that implements LLAP in a readable way.
- As a stepping stone to having a full userland AppleTalk stack aimed at serving vintage Macs.


## Definitions

The **overlay network** is the virtual LocalTalk network.

The **base network** is the IP network over which LLAP packets are transmitted.


## General Approach

Each LLAP packet to be sent, except for those involved in collision detection, is wrapped in a UDP packet along with enough information for protocol speakers to detect when their own packets are sent back at them. This packet is then sent out either as a broadcast on the local broadcast domain or as multicast to a specific multicast group (though on a single network, all LToUDP speakers must either be using one or the other).

When an LToUDP stack receives a UDP packet, it checks to see whether it is a packet it generated itself. If so, it discards it. If not, it passes it up to the LLAP stack for processing.


## Collision Detection

Since LocalTalk runs over a shared medium, collision detection is considerably important to it. In a world where we are tunneling LocalTalk over IP, it isn't important: we can trust the base network to do that job for us (and if we can't, we're knackered before we even start).

Therefore, **LLAP RTS** (request to send) **and CTS** (clear to send) **packets must never be transmitted over LToUDP.** If you are bridging an existing LocalTalk environment to LToUDP (for example, in an emulator), it is the responsibility of the bridging code to generate appropriate CTS packets to send back to the LocalTalk speaker to manage collision detection.

**All other LLAP packets may be transmitted over LToUDP.**


## Packet Anatomy

An LToUDP packet is a standard UDP packet aimed at port 1954. The headers contain nothing special. The payload of the packet contains:

```
   <--| 8 bits |-->   
+--------------------+
|                    |
+-                  -+
|     Sender ID      |
+-    (32 bits)     -+
|                    |
+-                  -+
|                    |
+--------------------+
|                    |
|                    |
|                    |
|    LLAP Packet     |
|    (as long as     |
|     necessary)     |
|                    |
|                    |
|                    |
|                    |
+--------------------+
```

**The payload begins with a 32-bit sender ID.** This is an opaque identifier that uniquely identifies one sender among potentially many from the same source IP address. It does not not need to be globally unique: it is valid for two different LToUDP speakers to have the same sender ID so long as they are sending packets from different source IP addresses.

On UNIX-like operating systems I personally use the process ID for this sender ID. On a single-tasking operating system where one is only expecting one LocalTalk speaker to run, it is reasonable to leave this field as zero (_or to embed a silly easter egg for people who are poking at it with Wireshark_).

**The rest of the payload is occupied by the LLAP packet.** Note that this is the _packet_, not the _frame_ (see _Inside Appletalk, second edition_, p. 1-7). The frame includes the SDLC gubbins that the LocalTalk hardware uses to detect the arrival of packets and check that they are intact. LToUDP speakers don't care about the mechanics of serial transmission, and UDP provides a checksum so we do not need to transmit the frame check sequence.


## Receiving Packets

To receive LToUDP packets, a normal UDP socket is used. Packets are received at port 1954. SO_RESUEADDR and SO_REUSEPORT are a good idea. If multicast is in use (which I recommend due to broadcast oddities on some operating systems) then the socket should join the appropriate multicast group. (The normal group is 239.192.76.84; this is an administratively-scoped multicast group where the last two octets are LT in ASCII, thus providing a mnemonic. If you're rolling this out on a larger, more organised multicast network—firstly, _why?_—and secondly, you will want to make sure this multicast group doesn't collide with anything else, and perhaps use a different one).

When a UDP packet is received to this socket, you must check to see if it is one of yours:

- Strip off the 32 bits of the sender ID: if this sender ID is not the sender ID that this process is embedding into the packets it sends, it's not one of your own that has come back to you. Pass it across to the LocalTalk stack.
- If it **is** the sender ID that this process is embedding into the packets it sends, check whether the source IP of the packet is from any IP address bound to any interface on the host. If it isn't, then it's not one of your own that has come back to you. Pass it across to the LocalTalk stack.
- If both the sender ID and the source IP match, then discard the packet.


## Sending Packets

To transmit LToUDP packets, a normal UDP socket is used. If multicast is in use, then the destination address of the packet must be the multicast group. If broadcast is in use, then the destination address of the packet must be the broadcast address. The destination port of the packet should be 1954. The source port of the packet is unimportant.

As noted above, if the LLAP packet is an RTS, then the LToUDP stack should respond with a synthesised CTS and not transmit the RTS over the UDP socket. If the LLAP packet is a CTS, it should be ignored and also not transmitted over the UDP socket.

Assuming the LLAP packet is neither an RTS nor a CTS packet, the first four bytes of the UDP payload must be the sender ID. After this should come the LLAP packet to be sent.
