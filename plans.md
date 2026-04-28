# Future plans for project.

1. Rename project to ClassicStack.
2. Add catsearch support in local_fs.
3. Add initial protocol support for IPX over rawlinks (pcap and tap). 
4. Add initial netbios support, over IPX and direct frames (aka NetBEUI) via our direct link. 
5. Add basic SMB 1.0 file server.
6. Add HTTP Proxy support. 
  - Support legacy SSL 3 to enable "encrypted" comms with Netscape 2.x
  - dynamic image resizing for memory constrained environments. 
  - css/javascript stripping
  - text-encoding/re-encoding (eg utf-8 to macroman)
7. add fsnotify to keep databases and file name mappings in sync. ie cnid.
8. implement an internal service bus for file create, rename/move and delete to allow other backends to handle/update their internal implementations. 
9. command line tools for echo, show detected nodes, afp client, etc?
10. alternate appledouble/sidecar. eg elliotnunns macresources format. https://github.com/elliotnunn/macresources. Not sure the best approach for byte-level access. 
11. AFP Printing support to text/Postscript/PDF
12. SMB printing support to text/postscript/PDF


Crazy ideas:
 - Interlink server support of null modem
 - PPP support, again over serial
 - NCP ?
 - MacIPX supports encapsulating IPX frames inside AppleTalk frames (MacIPX AppleTalk)
 - Novell shipped something called MACIPXGW.LAN which could bridge IPX networks to AppleTalk networks. MacIPX will detect such a gateway automatically.
 - would probably need to stand up a nw 3.1 server with appletalk to test. 
 - add protocol dumper supoprt, that writes pcap files on request. either as a standalone app or a config option for logging traffic to pcap file.

