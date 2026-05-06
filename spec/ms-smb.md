**\[MS-SMB\]:**

**Server Message Block (SMB) Protocol**

Intellectual Property Rights Notice for Open Specifications Documentation

- **Technical Documentation.** Microsoft publishes Open Specifications documentation ("this documentation") for protocols, file formats, data portability, computer languages, and standards support. Additionally, overview documents cover inter-protocol relationships and interactions.
- **Copyrights**. This documentation is covered by Microsoft copyrights. Regardless of any other terms that are contained in the terms of use for the Microsoft website that hosts this documentation, you can make copies of it in order to develop implementations of the technologies that are described in this documentation and can distribute portions of it in your implementations that use these technologies or in your documentation as necessary to properly document the implementation. You can also distribute in your implementation, with or without modification, any schemas, IDLs, or code samples that are included in the documentation. This permission also applies to any documents that are referenced in the Open Specifications documentation.
- **No Trade Secrets**. Microsoft does not claim any trade secret rights in this documentation.
- **Patents**. Microsoft has patents that might cover your implementations of the technologies described in the Open Specifications documentation. Neither this notice nor Microsoft's delivery of this documentation grants any licenses under those patents or any other Microsoft patents. However, a given Open Specifications document might be covered by the Microsoft [Open Specifications Promise](http://go.microsoft.com/fwlink/?LinkId=214445) or the [Microsoft Community Promise](http://go.microsoft.com/fwlink/?LinkId=214448). If you would prefer a written license, or if the technologies described in this documentation are not covered by the Open Specifications Promise or Community Promise, as applicable, patent licenses are available by contacting [iplg@microsoft.com](mailto:iplg@microsoft.com).
- **Trademarks**. The names of companies and products contained in this documentation might be covered by trademarks or similar intellectual property rights. This notice does not grant any licenses under those rights. For a list of Microsoft trademarks, visit [www.microsoft.com/trademarks](http://www.microsoft.com/trademarks).
- **Fictitious Names**. The example companies, organizations, products, domain names, email addresses, logos, people, places, and events that are depicted in this documentation are fictitious. No association with any real company, organization, product, domain name, email address, logo, person, place, or event is intended or should be inferred.

**Reservation of Rights**. All other rights are reserved, and this notice does not grant any rights other than as specifically described above, whether by implication, estoppel, or otherwise.

**Tools**. The Open Specifications documentation does not require the use of Microsoft programming tools or programming environments in order for you to develop an implementation. If you have access to Microsoft programming tools and environments, you are free to take advantage of them. Certain Open Specifications documents are intended for use in conjunction with publicly available standards specifications and network programming art and, as such, assume that the reader either is familiar with the aforementioned material or has immediate access to it.

**Revision Summary**

| Date       | Revision History | Revision Class | Comments                                                                     |
| ---------- | ---------------- | -------------- | ---------------------------------------------------------------------------- |
| 4/3/2007   | 0.01             | New            | Version 0.01 release                                                         |
| 7/3/2007   | 1.0              | Major          | MLonghorn+90                                                                 |
| 7/20/2007  | 2.0              | Major          | Updated and revised the technical content.                                   |
| 8/10/2007  | 3.0              | Major          | Updated and revised the technical content.                                   |
| 9/28/2007  | 4.0              | Major          | Updated and revised the technical content.                                   |
| 10/23/2007 | 5.0              | Major          | Updated and revised the technical content.                                   |
| 11/30/2007 | 5.0.1            | Editorial      | Changed language and formatting in the technical content.                    |
| 1/25/2008  | 5.0.2            | Editorial      | Changed language and formatting in the technical content.                    |
| 3/14/2008  | 5.0.3            | Editorial      | Changed language and formatting in the technical content.                    |
| 5/16/2008  | 6.0              | Major          | Updated and revised the technical content.                                   |
| 6/20/2008  | 7.0              | Major          | Updated and revised the technical content.                                   |
| 7/25/2008  | 8.0              | Major          | Updated and revised the technical content.                                   |
| 8/29/2008  | 9.0              | Major          | Updated and revised the technical content.                                   |
| 10/24/2008 | 10.0             | Major          | Updated and revised the technical content.                                   |
| 12/5/2008  | 11.0             | Major          | Updated and revised the technical content.                                   |
| 1/16/2009  | 12.0             | Major          | Updated and revised the technical content.                                   |
| 2/27/2009  | 13.0             | Major          | Updated and revised the technical content.                                   |
| 4/10/2009  | 14.0             | Major          | Updated and revised the technical content.                                   |
| 5/22/2009  | 15.0             | Major          | Updated and revised the technical content.                                   |
| 7/2/2009   | 16.0             | Major          | Updated and revised the technical content.                                   |
| 8/14/2009  | 17.0             | Major          | Updated and revised the technical content.                                   |
| 9/25/2009  | 18.0             | Major          | Updated and revised the technical content.                                   |
| 11/6/2009  | 19.0             | Major          | Updated and revised the technical content.                                   |
| 12/18/2009 | 20.0             | Major          | Updated and revised the technical content.                                   |
| 1/29/2010  | 21.0             | Major          | Updated and revised the technical content.                                   |
| 3/12/2010  | 22.0             | Major          | Updated and revised the technical content.                                   |
| 4/23/2010  | 23.0             | Major          | Updated and revised the technical content.                                   |
| 6/4/2010   | 24.0             | Major          | Updated and revised the technical content.                                   |
| 7/16/2010  | 25.0             | Major          | Updated and revised the technical content.                                   |
| 8/27/2010  | 26.0             | Major          | Updated and revised the technical content.                                   |
| 10/8/2010  | 27.0             | Major          | Updated and revised the technical content.                                   |
| 11/19/2010 | 28.0             | Major          | Updated and revised the technical content.                                   |
| 1/7/2011   | 29.0             | Major          | Updated and revised the technical content.                                   |
| 2/11/2011  | 30.0             | Major          | Updated and revised the technical content.                                   |
| 3/25/2011  | 31.0             | Major          | Updated and revised the technical content.                                   |
| 5/6/2011   | 32.0             | Major          | Updated and revised the technical content.                                   |
| 6/17/2011  | 33.0             | Major          | Updated and revised the technical content.                                   |
| 9/23/2011  | 34.0             | Major          | Updated and revised the technical content.                                   |
| 12/16/2011 | 35.0             | Major          | Updated and revised the technical content.                                   |
| 3/30/2012  | 36.0             | Major          | Updated and revised the technical content.                                   |
| 7/12/2012  | 37.0             | Major          | Updated and revised the technical content.                                   |
| 10/25/2012 | 38.0             | Major          | Updated and revised the technical content.                                   |
| 1/31/2013  | 39.0             | Major          | Updated and revised the technical content.                                   |
| 8/8/2013   | 40.0             | Major          | Updated and revised the technical content.                                   |
| 11/14/2013 | 41.0             | Major          | Updated and revised the technical content.                                   |
| 2/13/2014  | 42.0             | Major          | Updated and revised the technical content.                                   |
| 5/15/2014  | 43.0             | Major          | Updated and revised the technical content.                                   |
| 6/30/2015  | 44.0             | Major          | Significantly changed the technical content.                                 |
| 10/16/2015 | 44.0             | None           | No changes to the meaning, language, or formatting of the technical content. |
| 7/14/2016  | 45.0             | Major          | Significantly changed the technical content.                                 |

Table of Contents

[1 Introduction 9](#_Toc456184877)

[1.1 Glossary 9](#_Toc456184878)

[1.2 References 14](#_Toc456184879)

[1.2.1 Normative References 14](#_Toc456184880)

[1.2.2 Informative References 15](#_Toc456184881)

[1.3 Overview 16](#_Toc456184882)

[1.4 Relationship to Other Protocols 16](#_Toc456184883)

[1.5 Prerequisites/Preconditions 18](#_Toc456184884)

[1.6 Applicability Statement 18](#_Toc456184885)

[1.7 Versioning and Capability Negotiation 19](#_Toc456184886)

[1.8 Vendor-Extensible Fields 19](#_Toc456184887)

[1.9 Standards Assignments 19](#_Toc456184888)

[2 Messages 21](#_Toc456184889)

[2.1 Transport 21](#_Toc456184890)

[2.2 Message Syntax 21](#_Toc456184891)

[2.2.1 Common Data Type Extensions 22](#_Toc456184892)

[2.2.1.1 Character Sequences 22](#_Toc456184893)

[2.2.1.1.1 Pathname Extensions 22](#_Toc456184894)

[2.2.1.2 File Attributes 23](#_Toc456184895)

[2.2.1.2.1 Extended File Attribute (SMB_EXT_FILE_ATTR) Extensions 23](#_Toc456184896)

[2.2.1.2.2 File System Attribute Extensions 24](#_Toc456184897)

[2.2.1.3 Unique Identifiers 25](#_Toc456184898)

[2.2.1.3.1 FileId Generation 26](#_Toc456184899)

[2.2.1.3.2 VolumeGUID Generation 26](#_Toc456184900)

[2.2.1.3.3 Copychunk Resume Key Generation 26](#_Toc456184901)

[2.2.1.4 Access Masks 26](#_Toc456184902)

[2.2.1.4.1 File_Pipe_Printer_Access_Mask 26](#_Toc456184903)

[2.2.1.4.2 Directory_Access_Mask 28](#_Toc456184904)

[2.2.2 Defined Constant Extensions 30](#_Toc456184905)

[2.2.2.1 SMB_COM Command Codes 30](#_Toc456184906)

[2.2.2.2 Transaction Subcommand Codes 30](#_Toc456184907)

[2.2.2.3 Information Level Codes 30](#_Toc456184908)

[2.2.2.3.1 FIND Information Level Codes 30](#_Toc456184909)

[2.2.2.3.2 QUERY_FS Information Level Codes 31](#_Toc456184910)

[2.2.2.3.3 QUERY Information Level Codes 31](#_Toc456184911)

[2.2.2.3.4 SET Information Level Codes 31](#_Toc456184912)

[2.2.2.3.5 Pass-through Information Level Codes 31](#_Toc456184913)

[2.2.2.3.6 Other Information Level Codes 31](#_Toc456184914)

[2.2.2.4 SMB Error Classes and Codes 31](#_Toc456184915)

[2.2.2.5 Session Key Protection Hash 33](#_Toc456184916)

[2.2.3 SMB Message Structure Extensions 34](#_Toc456184917)

[2.2.3.1 SMB Header Extensions 34](#_Toc456184918)

[2.2.4 SMB Command Extensions 36](#_Toc456184919)

[2.2.4.1 SMB_COM_OPEN_ANDX (0x2D) 36](#_Toc456184920)

[2.2.4.1.1 Client Request Extensions 36](#_Toc456184921)

[2.2.4.1.2 Server Response Extensions 37](#_Toc456184922)

[2.2.4.2 SMB_COM_READ_ANDX (0x2E) 39](#_Toc456184923)

[2.2.4.2.1 Client Request Extensions 39](#_Toc456184924)

[2.2.4.2.2 Server Response Extensions 41](#_Toc456184925)

[2.2.4.3 SMB_COM_WRITE_ANDX (0x2F) 42](#_Toc456184926)

[2.2.4.3.1 Client Request Extensions 42](#_Toc456184927)

[2.2.4.3.2 Server Response Extensions 43](#_Toc456184928)

[2.2.4.4 SMB_COM_TRANSACTION2 (0x32) Extensions 44](#_Toc456184929)

[2.2.4.5 SMB_COM_NEGOTIATE (0x72) 44](#_Toc456184930)

[2.2.4.5.1 Client Request Extensions 44](#_Toc456184931)

[2.2.4.5.2 Server Response Extensions 44](#_Toc456184932)

[2.2.4.5.2.1 Extended Security Response 44](#_Toc456184933)

[2.2.4.5.2.2 Non-Extended Security Response 49](#_Toc456184934)

[2.2.4.6 SMB_COM_SESSION_SETUP_ANDX (0x73) 52](#_Toc456184935)

[2.2.4.6.1 Client Request Extensions 52](#_Toc456184936)

[2.2.4.6.2 Server Response Extensions 54](#_Toc456184937)

[2.2.4.7 SMB_COM_TREE_CONNECT_ANDX (0x75) 57](#_Toc456184938)

[2.2.4.7.1 Client Request Extensions 57](#_Toc456184939)

[2.2.4.7.2 Server Response Extensions 58](#_Toc456184940)

[2.2.4.8 SMB_COM_NT_TRANSACT (0xA0) Extensions 60](#_Toc456184941)

[2.2.4.9 SMB_COM_NT_CREATE_ANDX (0xA2) 60](#_Toc456184942)

[2.2.4.9.1 Client Request Extensions 60](#_Toc456184943)

[2.2.4.9.2 Server Response Extensions 62](#_Toc456184944)

[2.2.4.10 SMB_COM_SEARCH (0x81) Extensions 65](#_Toc456184945)

[2.2.5 Transaction Subcommand Extensions 66](#_Toc456184946)

[2.2.5.1 TRANS_RAW_READ_NMPIPE (0x0011) 66](#_Toc456184947)

[2.2.5.2 TRANS_CALL_NMPIPE (0x0054) 66](#_Toc456184948)

[2.2.6 Transaction 2 Subcommand Extensions 66](#_Toc456184949)

[2.2.6.1 TRANS2_FIND_FIRST2 (0x0001) 66](#_Toc456184950)

[2.2.6.1.1 Client Request Extensions 66](#_Toc456184951)

[2.2.6.1.2 Server Response Extensions 66](#_Toc456184952)

[2.2.6.2 TRANS2_FIND_NEXT2 (0x0002) 67](#_Toc456184953)

[2.2.6.2.1 Client Request Extensions 67](#_Toc456184954)

[2.2.6.2.2 Server Response Extensions 67](#_Toc456184955)

[2.2.6.3 TRANS2_QUERY_FS_INFORMATION (0x0003) 67](#_Toc456184956)

[2.2.6.3.1 Client Request Extensions 67](#_Toc456184957)

[2.2.6.3.2 Server Response Extensions 67](#_Toc456184958)

[2.2.6.4 TRANS2_SET_FS_INFORMATION (0x0004) 67](#_Toc456184959)

[2.2.6.4.1 Client Request 67](#_Toc456184960)

[2.2.6.4.2 Server Response 68](#_Toc456184961)

[2.2.6.5 TRANS2_QUERY_PATH_INFORMATION (0x0005) 69](#_Toc456184962)

[2.2.6.5.1 Client Request Extensions 69](#_Toc456184963)

[2.2.6.5.2 Server Response Extensions 70](#_Toc456184964)

[2.2.6.6 TRANS2_SET_PATH_INFORMATION (0x0006) 70](#_Toc456184965)

[2.2.6.6.1 Client Request Extensions 70](#_Toc456184966)

[2.2.6.6.2 Server Response Extensions 70](#_Toc456184967)

[2.2.6.7 TRANS2_QUERY_FILE_INFORMATION (0x0007) 70](#_Toc456184968)

[2.2.6.7.1 Client Request Extensions 70](#_Toc456184969)

[2.2.6.7.2 Server Response Extensions 70](#_Toc456184970)

[2.2.6.8 TRANS2_SET_FILE_INFORMATION (0x0008) 70](#_Toc456184971)

[2.2.6.8.1 Client Request Extensions 70](#_Toc456184972)

[2.2.6.8.2 Server Response Extensions 71](#_Toc456184973)

[2.2.7 NT Transact Subcommand Extensions 71](#_Toc456184974)

[2.2.7.1 NT_TRANSACT_CREATE (0x0001) Extensions 71](#_Toc456184975)

[2.2.7.1.1 Client Request Extensions 71](#_Toc456184976)

[2.2.7.1.2 Server Response Extensions 73](#_Toc456184977)

[2.2.7.2 NT_TRANSACT_IOCTL (0x0002) 76](#_Toc456184978)

[2.2.7.2.1 Client Request Extensions 76](#_Toc456184979)

[2.2.7.2.1.1 SRV_COPYCHUNK 78](#_Toc456184980)

[2.2.7.2.2 Server Response Extensions 79](#_Toc456184981)

[2.2.7.2.2.1 FSCTL_SRV_ENUMERATE_SNAPSHOTS Response 79](#_Toc456184982)

[2.2.7.2.2.2 FSCTL_SRV_REQUEST_RESUME_KEY Response 80](#_Toc456184983)

[2.2.7.2.2.3 FSCTL_SRV_COPYCHUNK Response 81](#_Toc456184984)

[2.2.7.3 NT_TRANSACT_SET_SECURITY_DESC (0x0003) Extensions 82](#_Toc456184985)

[2.2.7.4 NT_TRANSACT_QUERY_SECURITY_DESC (0x0006) Extensions 83](#_Toc456184986)

[2.2.7.5 NT_TRANSACT_QUERY_QUOTA (0x0007) 83](#_Toc456184987)

[2.2.7.5.1 Client Request 84](#_Toc456184988)

[2.2.7.5.2 Server Response 85](#_Toc456184989)

[2.2.7.6 NT_TRANSACT_SET_QUOTA (0x0008) 87](#_Toc456184990)

[2.2.7.6.1 Client Request 87](#_Toc456184991)

[2.2.7.6.2 Server Response 88](#_Toc456184992)

[2.2.8 Information Levels 89](#_Toc456184993)

[2.2.8.1 FIND Information Level Extensions 90](#_Toc456184994)

[2.2.8.1.1 SMB_FIND_FILE_BOTH_DIRECTORY_INFO Extensions 90](#_Toc456184995)

[2.2.8.1.2 SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO 92](#_Toc456184996)

[2.2.8.1.3 SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO 93](#_Toc456184997)

[2.2.8.2 QUERY_FS Information Level Extensions 95](#_Toc456184998)

[2.2.8.2.1 SMB_QUERY_FS_ATTRIBUTE_INFO 95](#_Toc456184999)

[2.2.8.3 QUERY Information Level Extensions 95](#_Toc456185000)

[2.2.8.4 SET Information level Extensions 96](#_Toc456185001)

[3 Protocol Details 97](#_Toc456185002)

[3.1 Common Details 97](#_Toc456185003)

[3.1.1 Abstract Data Model 97](#_Toc456185004)

[3.1.1.1 Global 97](#_Toc456185005)

[3.1.2 Timers 97](#_Toc456185006)

[3.1.3 Initialization 97](#_Toc456185007)

[3.1.4 Higher-Layer Triggered Events 97](#_Toc456185008)

[3.1.4.1 Sending Any Message 97](#_Toc456185009)

[3.1.5 Message Processing Events and Sequencing Rules 98](#_Toc456185010)

[3.1.5.1 Receiving Any Message 98](#_Toc456185011)

[3.1.6 Timer Events 98](#_Toc456185012)

[3.1.7 Other Local Events 98](#_Toc456185013)

[3.2 Client Details 98](#_Toc456185014)

[3.2.1 Abstract Data Model 98](#_Toc456185015)

[3.2.1.1 Global 98](#_Toc456185016)

[3.2.1.2 Per SMB Connection 99](#_Toc456185017)

[3.2.1.3 Per SMB Session 99](#_Toc456185018)

[3.2.1.4 Per Tree Connect 99](#_Toc456185019)

[3.2.1.5 Per Unique Open 99](#_Toc456185020)

[3.2.2 Timers 99](#_Toc456185021)

[3.2.3 Initialization 100](#_Toc456185022)

[3.2.4 Higher-Layer Triggered Events 100](#_Toc456185023)

[3.2.4.1 Sending Any Message 100](#_Toc456185024)

[3.2.4.1.1 Scanning a Path for a Previous Version Token 100](#_Toc456185025)

[3.2.4.2 Application Requests Connecting to a Share 100](#_Toc456185026)

[3.2.4.2.1 Connection Establishment 100](#_Toc456185027)

[3.2.4.2.2 Dialect Negotiation 100](#_Toc456185028)

[3.2.4.2.3 Capabilities Negotiation 101](#_Toc456185029)

[3.2.4.2.4 User Authentication 101](#_Toc456185030)

[3.2.4.2.4.1 Sequence Diagram 102](#_Toc456185031)

[3.2.4.2.5 Connecting to the Share (Tree Connect) 104](#_Toc456185032)

[3.2.4.3 Application Requests Opening a File 104](#_Toc456185033)

[3.2.4.3.1 SMB_COM_NT_CREATE_ANDX Request 105](#_Toc456185034)

[3.2.4.3.2 SMB_COM_OPEN_ANDX Request (deprecated) 105](#_Toc456185035)

[3.2.4.4 Application Requests Reading from a File, Named Pipe, or Device 105](#_Toc456185036)

[3.2.4.4.1 Large Read Support 105](#_Toc456185037)

[3.2.4.5 Application Requests Writing to a File, Named Pipe, or Device 106](#_Toc456185038)

[3.2.4.6 Application Requests a Directory Enumeration 106](#_Toc456185039)

[3.2.4.7 Application Requests Querying File Attributes 106](#_Toc456185040)

[3.2.4.8 Application Requests Setting File Attributes 107](#_Toc456185041)

[3.2.4.9 Application Requests Querying File System Attributes 107](#_Toc456185042)

[3.2.4.10 Application Requests Setting File System Attributes 108](#_Toc456185043)

[3.2.4.11 Application Requests Sending an I/O Control to a File or Device 108](#_Toc456185044)

[3.2.4.11.1 Application Requests Enumerating Available Previous Versions 108](#_Toc456185045)

[3.2.4.11.2 Performing a Server-Side Data Copy 108](#_Toc456185046)

[3.2.4.11.2.1 Application queries the Copychunk Resume Key of the Source File 109](#_Toc456185047)

[3.2.4.11.2.2 Application requests a Server-side Data Copy 109](#_Toc456185048)

[3.2.4.12 Application Requests Querying of DFS Referral 110](#_Toc456185049)

[3.2.4.13 Application Requests Querying User Quota Information 110](#_Toc456185050)

[3.2.4.14 Application Requests Setting User Quota Information 111](#_Toc456185051)

[3.2.4.15 Application Requests the Session Key for a Connection 111](#_Toc456185052)

[3.2.5 Message Processing Events and Sequencing Rules 112](#_Toc456185053)

[3.2.5.1 Receiving Any Message 112](#_Toc456185054)

[3.2.5.2 Receiving an SMB_COM_NEGOTIATE Response 112](#_Toc456185055)

[3.2.5.3 Receiving an SMB_COM_SESSION_SETUP_ANDX Response 112](#_Toc456185056)

[3.2.5.4 Receiving an SMB_COM_TREE_CONNECT_ANDX Response 114](#_Toc456185057)

[3.2.5.5 Receiving an SMB_COM_NT_CREATE_ANDX Response 115](#_Toc456185058)

[3.2.5.6 Receiving an SMB_COM_OPEN_ANDX Response 115](#_Toc456185059)

[3.2.5.7 Receiving an SMB_COM_READ_ANDX Response 115](#_Toc456185060)

[3.2.5.8 Receiving an SMB_COM_WRITE_ANDX Response 116](#_Toc456185061)

[3.2.5.9 Receiving any SMB_COM_NT_TRANSACT Response 116](#_Toc456185062)

[3.2.5.9.1 Receiving an NT_TRANSACT_IOCTL Response 116](#_Toc456185063)

[3.2.5.9.1.1 Receiving an FSCTL_SRV_REQUEST_RESUME_KEY Function Code 116](#_Toc456185064)

[3.2.5.9.1.2 Receiving an FSCTL_SRV_COPYCHUNK Function Code 116](#_Toc456185065)

[3.2.5.9.2 Receiving an NT_TRANSACT_QUERY_QUOTA Response 116](#_Toc456185066)

[3.2.5.9.3 Receiving an NT_TRANSACT_SET_QUOTA Response 116](#_Toc456185067)

[3.2.5.10 Receiving an SMB_COM_SEARCH Response 116](#_Toc456185068)

[3.2.5.11 Receiving any SMB_COM_TRANSACTION2 subcommand Response 117](#_Toc456185069)

[3.2.5.11.1 Receiving any TRANS2_SET_FS_INFORMATION Response 117](#_Toc456185070)

[3.2.6 Timer Events 117](#_Toc456185071)

[3.2.7 Other Local Events 117](#_Toc456185072)

[3.3 Server Details 117](#_Toc456185073)

[3.3.1 Abstract Data Model 117](#_Toc456185074)

[3.3.1.1 Global 117](#_Toc456185075)

[3.3.1.2 Per Share 118](#_Toc456185076)

[3.3.1.3 Per SMB Connection 118](#_Toc456185077)

[3.3.1.4 Per Pending SMB Command 118](#_Toc456185078)

[3.3.1.5 Per SMB Session 118](#_Toc456185079)

[3.3.1.6 Per Tree Connect 118](#_Toc456185080)

[3.3.1.7 Per Unique Open 118](#_Toc456185081)

[3.3.2 Timers 118](#_Toc456185082)

[3.3.2.1 Authentication Expiration Timer 118](#_Toc456185083)

[3.3.3 Initialization 119](#_Toc456185084)

[3.3.4 Higher-Layer Triggered Events 119](#_Toc456185085)

[3.3.4.1 Sending Any Message 119](#_Toc456185086)

[3.3.4.1.1 Sending Any Error Response Message 119](#_Toc456185087)

[3.3.4.2 Server Application Queries a User Session Key 119](#_Toc456185088)

[3.3.4.3 DFS Server Notifies SMB Server That DFS Is Active 120](#_Toc456185089)

[3.3.4.4 DFS Server Notifies SMB Server That a Share Is a DFS Share 120](#_Toc456185090)

[3.3.4.5 DFS Server Notifies SMB Server That a Share Is Not a DFS Share 120](#_Toc456185091)

[3.3.4.6 Server Application Updates a Share 120](#_Toc456185092)

[3.3.4.7 Server Application Requests Querying a Share 120](#_Toc456185093)

[3.3.5 Message Processing Events and Sequencing Rules 121](#_Toc456185094)

[3.3.5.1 Receiving Any Message 121](#_Toc456185095)

[3.3.5.1.1 Scanning a Path for a Previous Version Token 122](#_Toc456185096)

[3.3.5.1.2 Granting Oplocks 122](#_Toc456185097)

[3.3.5.2 Receiving an SMB_COM_NEGOTIATE Request 122](#_Toc456185098)

[3.3.5.3 Receiving an SMB_COM_SESSION_SETUP_ANDX Request 123](#_Toc456185099)

[3.3.5.4 Receiving an SMB_COM_TREE_CONNECT_ANDX Request 125](#_Toc456185100)

[3.3.5.5 Receiving an SMB_COM_NT_CREATE_ANDX Request 126](#_Toc456185101)

[3.3.5.6 Receiving an SMB_COM_OPEN_ANDX Request 127](#_Toc456185102)

[3.3.5.7 Receiving an SMB_COM_READ_ANDX Request 128](#_Toc456185103)

[3.3.5.8 Receiving an SMB_COM_WRITE_ANDX Request 128](#_Toc456185104)

[3.3.5.9 Receiving an SMB_COM_SEARCH Request 129](#_Toc456185105)

[3.3.5.10 Receiving any SMB_COM_TRANSACTION2 subcommand 129](#_Toc456185106)

[3.3.5.10.1 Receiving any Information Level 129](#_Toc456185107)

[3.3.5.10.2 Receiving a TRANS2_FIND_FIRST2 Request 129](#_Toc456185108)

[3.3.5.10.3 Receiving a TRANS2_FIND_NEXT2 Request 129](#_Toc456185109)

[3.3.5.10.4 Receiving a TRANS2_QUERY_FILE_INFORMATION Request 130](#_Toc456185110)

[3.3.5.10.5 Receiving a TRANS2_QUERY_PATH_INFORMATION Request 130](#_Toc456185111)

[3.3.5.10.6 Receiving a TRANS2_SET_FILE_INFORMATION Request 130](#_Toc456185112)

[3.3.5.10.7 Receiving a TRANS2_SET_PATH_INFORMATION Request 130](#_Toc456185113)

[3.3.5.10.8 Receiving a TRANS2_QUERY_FS_INFORMATION Request 130](#_Toc456185114)

[3.3.5.10.9 Receiving a TRANS2_SET_FS_INFORMATION Request 130](#_Toc456185115)

[3.3.5.11 Receiving any SMB_COM_NT_TRANSACT Subcommand 130](#_Toc456185116)

[3.3.5.11.1 Receiving an NT_TRANSACT_IOCTL Request 130](#_Toc456185117)

[3.3.5.11.1.1 Receiving an FSCTL_SRV_ENUMERATE_SNAPSHOTS Function Code 131](#_Toc456185118)

[3.3.5.11.1.2 Receiving an FSCTL_SRV_REQUEST_RESUME_KEY Function Code 131](#_Toc456185119)

[3.3.5.11.1.3 Receiving an FSCTL_SRV_COPYCHUNK Request 131](#_Toc456185120)

[3.3.5.11.2 Receiving an NT_TRANS_QUERY_QUOTA Request 132](#_Toc456185121)

[3.3.5.11.3 Receiving an NT_TRANS_SET_QUOTA Request 132](#_Toc456185122)

[3.3.5.11.4 Receiving an NT_TRANSACT_CREATE Request 133](#_Toc456185123)

[3.3.6 Timer Events 133](#_Toc456185124)

[3.3.6.1 Authentication Expiration Timer Event 133](#_Toc456185125)

[3.3.7 Other Local Events 133](#_Toc456185126)

[4 Protocol Examples 134](#_Toc456185127)

[4.1 Extended Security Authentication 134](#_Toc456185128)

[4.2 Previous File Version Enumeration 136](#_Toc456185129)

[4.3 Message Signing Example 139](#_Toc456185130)

[4.4 Copy File (Remote to Local) 141](#_Toc456185131)

[4.5 Copy File (Local to Remote) 144](#_Toc456185132)

[4.6 FSCTL SRV COPYCHUNK 147](#_Toc456185133)

[4.7 TRANS TRANSACT NMPIPE 153](#_Toc456185134)

[5 Security 157](#_Toc456185135)

[5.1 Security Considerations for Implementers 157](#_Toc456185136)

[5.2 Index of Security Parameters 157](#_Toc456185137)

[6 Appendix A: Product Behavior 158](#_Toc456185138)

[7 Change Tracking 177](#_Toc456185139)

[8 Index 179](#_Toc456185140)

# Introduction

The Server Message Block (SMB) Version 1.0 Protocol defines extensions to the Common Internet File System (CIFS) Protocol, which is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b). Unless specifically extended or overridden in this document, all specifications and behaviors that are described for Windows NT operating system clients and servers in \[MS-CIFS\] apply to the Windows client and server implementations covered in this document. The list of Windows client and server implementations covered in this document is provided in section [6](#Section_ecd51ae2478c455b8669254b74208d3b).

Unless otherwise noted, this document only provides the extensions made to the CIFS Protocol relative to the specification in \[MS-CIFS\]. The extended CIFS Protocol is known as the Server Message Block (SMB) Version 1.0 Protocol. Both this document and \[MS-CIFS\] are required in order to create a complete implementation of the Server Message Block (SMB) Version 1.0 Protocol.

This document also defines Windows behavior with respect to optional behavior that is described in the specifications of the SMB extensions.

Sections 1.5, 1.8, 1.9, 2, and 3 of this specification are normative. All other sections and examples in this specification are informative.

## Glossary

This document uses the following terms:

**@GMT token**: A special token that can be present as part of a file path to indicate a request to see a previous version of the file or directory. The format is "@GMT-YYYY.MM.DD-HH.MM.SS". This 16-bit [**Unicode string**](#gt_b069acb4-e364-453e-ac83-42d469bb339e) represents a time and date in [**Coordinated Universal Time (UTC)**](#gt_f2369991-a884-4843-a8fa-1505b6d5ece7), with YYYY representing the year, MM the month, DD the day, HH the hour, MM the minute, and SS the seconds.

**8.3 name**: A file name string restricted in length to 12 characters that includes a base name of up to eight characters, one character for a period, and up to three characters for a file name extension. For more information on 8.3 file names, see [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.1.1.1.

**byte mode**: One of two kinds of [**named pipe**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca), the other of which is [**message mode**](#gt_c49a48e8-f1ac-4568-bc87-0672eb08868b). In byte mode, the data sent or received on the named pipe does not have message boundaries but is treated as a continuous stream. \[XOPEN-SMB\] uses the term stream mode instead of byte mode, and [\[SMB-LM1X\]](http://go.microsoft.com/fwlink/?LinkId=164302) refers to byte mode as byte stream mode.

**Common Internet File System (CIFS)**: The "NT LM 0.12" / NT LAN Manager dialect of the [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) Protocol, as implemented in Windows NT. The CIFS name originated in the 1990's as part of an attempt to create an Internet standard for [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625), based upon the then-current Windows NT implementation.

**Coordinated Universal Time (UTC)**: A high-precision atomic time standard that approximately tracks Universal Time (UT). It is the basis for legal, civil time all over the Earth. Time zones around the world are expressed as positive and negative offsets from UTC. In this role, it is also referred to as Zulu time (Z) and Greenwich Mean Time (GMT). In these specifications, all references to UTC refer to the time at UTC-0 (or GMT).

**Copychunk Resume Key**: A 24-byte value generated by a [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server in response to a request by an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client that uniquely identifies an open file on the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server. A [**Copychunk Resume Key**](#gt_7a86a8b5-6ac4-471d-aa3c-9b2bb4638700) is used by [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server-side data movement operations between files without requiring the data to be read by the client and then written back to the server. Note that this is different from the resume key specified in \[MS-CIFS\] section 2.2.6.2 that is returned by the server in response to a TRANS2_FIND_FIRST2 subcommand of an SMB_COM_TRANSACTION2 client request.

**deprecated**: A deprecated feature is one that has been superseded in the protocol by a newer feature. Use of deprecated features is discouraged. Server implementations might need to implement deprecated features to support clients that negotiate earlier [**SMB dialects**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1).

**discretionary access control list (DACL)**: An access control list (ACL) that is controlled by the owner of an object and that specifies the access particular users or groups can have to the object.

**Distributed File System (DFS)**: A file system that logically groups physical shared folders located on different servers by transparently connecting them to one or more hierarchical namespaces. [**DFS**](#gt_0b8086c9-d025-45b8-bf09-6b5eca72713e) also provides fault-tolerance and load-sharing capabilities. [**DFS**](#gt_0b8086c9-d025-45b8-bf09-6b5eca72713e) refers to the Microsoft DFS available in Windows Server operating system platforms.

**domain**: A set of users and computers sharing a common namespace and management infrastructure. At least one computer member of the set must act as a domain controller (DC) and host a member list that identifies all members of the domain, as well as optionally hosting the Active Directory service. The domain controller provides authentication (2) of members, creating a unit of trust for its members. Each domain has an identifier that is shared among its members. For more information, see [\[MS-AUTHSOD\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AUTHSOD%5d.pdf#Section_953d700a57cb4cf7b0c3a64f34581cc9) section 1.1.1.5 and [\[MS-ADTS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-ADTS%5d.pdf#Section_d243592709994c628c6d13ba31a52e1a).

**Fid**: A 16-bit value that the [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server uses to represent an opened file, [**named pipe**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca), printer, or device. A [**Fid**](#gt_ab858f4d-7f0c-474c-9697-50f0af92f766) is returned by an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server in response to a client request to open or create a file, [**named pipe**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca), printer, or device. The [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server guarantees that the [**Fid**](#gt_ab858f4d-7f0c-474c-9697-50f0af92f766) value returned is unique for a given [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) connection until the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) connection is closed, at which time the [**Fid**](#gt_ab858f4d-7f0c-474c-9697-50f0af92f766) value can be reused. The [**Fid**](#gt_ab858f4d-7f0c-474c-9697-50f0af92f766) is used by the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client in subsequent [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) commands to identify the opened file, [**named pipe**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca), printer, or device.

**file allocation table (FAT)**: A data structure that the operating system creates when a volume is formatted by using [**FAT**](#gt_f2bf797b-e733-4fb9-b5e5-7e122f4abbe0) or FAT32 file systems. The operating system stores information about each file in the [**FAT**](#gt_f2bf797b-e733-4fb9-b5e5-7e122f4abbe0) so that it can retrieve the file later.

**file system control (FSCTL)**: A command issued to a file system to alter or query the behavior of the file system and/or set or query metadata that is associated with a particular file or with the file system itself.

**FileId**: A 64-bit value that is used to represent a file. The value of a [**FileId**](#gt_3b097896-b707-47b5-b1bb-384867a453ea) is unique on a single volume of a local file system or a remote file server. A [**FileId**](#gt_3b097896-b707-47b5-b1bb-384867a453ea) is not guaranteed to be unique across volumes, but the file system on the server must guarantee that it is unique within a given volume if [**FileIds**](#gt_3b097896-b707-47b5-b1bb-384867a453ea) are supported. [**FileIds**](#gt_3b097896-b707-47b5-b1bb-384867a453ea) are not supported by all local file systems. On Windows, [**NTFS**](#gt_86f79a17-c0be-4937-8660-0cf6ce5ddc1a) supports [**FileIds**](#gt_3b097896-b707-47b5-b1bb-384867a453ea), but the [**file allocation table (FAT)**](#gt_f2bf797b-e733-4fb9-b5e5-7e122f4abbe0) file system does not support them.

**guest account**: A security account available to users who do not have an account on the computer.

**I/O control (IOCTL)**: A command that is issued to a target file system or target device in order to query or alter the behavior of the target; or to query or alter the data and attributes that are associated with the target or the objects that are exposed by the target.

**information level**: A number used to identify the volume, file, or device information being requested by a client. Corresponding to each [**information level**](#gt_b01da706-86d0-4ee2-9461-2d9fb1060543), the server returns a specific structure to the client that contains different information in the response.

**Key Distribution Center (KDC)**: The Kerberos service that implements the authentication (2) and ticket granting services specified in the Kerberos protocol. The service runs on computers selected by the administrator of the realm or domain; it is not present on every machine on the network. It must have access to an account database for the realm that it serves. Windows [**KDCs**](#gt_6e5aafba-6b66-4fdd-872e-844f142af287) are integrated into the domain controller role of a Windows Server acting as a Domain Controller. It is a network service that supplies tickets to clients for use in authenticating to services.

**little-endian**: Multiple-byte values that are byte-ordered with the least significant byte stored in the memory location with the lowest address.

**message mode**: A named pipe can be of two types: byte mode or [**message mode**](#gt_c49a48e8-f1ac-4568-bc87-0672eb08868b). In byte mode, the data sent or received on the named pipe does not have message boundaries but is treated as a continuous Stream. In message mode, message boundaries are enforced.

**named pipe**: A named, one-way, or duplex pipe for communication between a pipe server and one or more pipe clients.

**network byte order**: The order in which the bytes of a multiple-byte number are transmitted on a network, most significant byte first (in big-endian storage). This may or may not match the order in which numbers are normally stored in memory for a particular processor.

**NT file system (NTFS)**: A proprietary Microsoft file system. For more information, see [\[MSFT-NTFS\]](http://go.microsoft.com/fwlink/?LinkId=90200).

**object store**: A system that provides the ability to create, query, modify, or apply policy to a local resource on behalf of a remote client. The object store is backed by a file system, a named pipe, or a print job that is accessed as a file.

**Obsolescent**: A feature that has no replacement but is becoming obsolete. Although the use of obsolescent features is discouraged, server implementations might need to implement them to support clients that negotiate earlier [**SMB dialects**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1).

**open**: A runtime object that corresponds to a currently established access to a specific file or a named pipe from a specific client to a specific server, using a specific user security context. Both clients and servers maintain opens that represent active accesses.

**oplock break**: An unsolicited request sent by a [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server to an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client to inform the client to change the [**oplock**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394) level for a file.

**opportunistic lock (oplock)**: A mechanism designed to allow clients to dynamically alter their buffering strategy in a consistent manner to increase performance and reduce network use. The network performance for remote file operations may be increased if a client can locally buffer file data, which reduces or eliminates the need to send and receive network packets. For example, a client may not have to write information into a file on a remote server if the client knows that no other process is accessing the data. Likewise, the client may buffer read-ahead data from the remote file if the client knows that no other process is writing data to the remote file. There are three types of [**oplocks**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394): Exclusive oplock allows a client to open a file for exclusive access and allows the client to perform arbitrary buffering. Batch oplock allows a client to keep a file open on the server even though the local accessor on the client machine has closed the file. Level II oplock indicates that there are multiple readers of a file and no writers. Level II Oplocks are supported if the negotiated SMB Dialect is NT LM 0.12 or later. When a client opens a file, it requests the server to grant it a particular type of [**oplock**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394) on the file. The response from the server indicates the type of [**oplock**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394) granted to the client. The client uses the granted [**oplock**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394) type to adjust its buffering policy.

**original equipment manufacturer (OEM) character**: An 8-bit encoding used in MS-DOS and Windows operating systems to associate a sequence of bits with specific characters. The ASCII character set maps the letters, numerals, and specified punctuation and control characters to the numbers from 0 to 127. The term "code page" is used to refer to extensions of the ASCII character set that map specified characters and symbols to the numbers from 128 to 255. These code pages are referred to as OEM character sets. For more information, see [\[MSCHARSET\]](http://go.microsoft.com/fwlink/?LinkId=89944).

**process identifier (PID)**: A nonzero integer used by some operating systems (for example, Windows and UNIX) to uniquely identify a process. For more information, see [\[PROCESS\]](http://go.microsoft.com/fwlink/?LinkId=90251).

**raw read (on a named pipe)**: The act of reading data from a [**named pipe**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca) that ignores message boundaries even if the pipe was set up as a [**message mode**](#gt_c49a48e8-f1ac-4568-bc87-0672eb08868b) pipe.

**reparse point**: An attribute that can be added to a file to store a collection of user-defined data that is opaque to [**NTFS**](#gt_86f79a17-c0be-4937-8660-0cf6ce5ddc1a) or ReFS. If a file that has a reparse point is opened, the open will normally fail with STATUS_REPARSE, so that the relevant file system filter driver can detect the open of a file associated with (owned by) this reparse point. At that point, each installed filter driver can check to see if it is the owner of the reparse point, and, if so, perform any special processing required for a file with that reparse point. The format of this data is understood by the application that stores the data and the file system filter that interprets the data and processes the file. For example, an encryption filter that is marked as the owner of a file's reparse point could look up the encryption key for that file. A file can have (at most) 1 reparse point associated with it. For more information, see [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e).

**security context**: An abstract data structure that contains authorization information for a particular security principal in the form of a Token/Authorization Context (see [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.5.2). A server uses the authorization information in a [**security context**](#gt_88d49f20-6c95-4b64-a52c-c3eca2fe5709) to check access to requested resources. A [**security context**](#gt_88d49f20-6c95-4b64-a52c-c3eca2fe5709) also contains a key identifier that associates mutually established cryptographic keys, along with other information needed to perform secure communication with another security principal.

**security descriptor**: A data structure containing the security information associated with a securable object. A [**security descriptor**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350) identifies an object's owner by its [**security identifier (SID)**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d). If access control is configured for the object, its [**security descriptor**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350) contains a [**discretionary access control list (DACL)**](#gt_d727f612-7a45-48e4-9d87-71735d62b321) with [**SIDs**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d) for the security principals who are allowed or denied access. Applications use this structure to set and query an object's security status. The [**security descriptor**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350) is used to guard access to an object as well as to control which type of auditing takes place when the object is accessed. The [**security descriptor**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350) format is specified in \[MS-DTYP\] section 2.4.6; a string representation of [**security descriptors**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350), called SDDL, is specified in \[MS-DTYP\] section 2.5.1.

**security identifier (SID)**: An identifier for security principals in Windows that is used to identify an account or a group. Conceptually, the [**SID**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d) is composed of an account authority portion (typically a [**domain**](#gt_b0276eb2-4e65-4cf1-a718-e0920a614aca)) and a smaller integer representing an identity relative to the account authority, termed the relative identifier (RID). The [**SID**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d) format is specified in \[MS-DTYP\] section 2.4.2; a string representation of [**SIDs**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d) is specified in \[MS-DTYP\] section 2.4.2 and [\[MS-AZOD\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AZOD%5d.pdf#Section_5a0a0a3ec7a742e1b5f2cc8d8bd9739e) section 1.1.1.2.

**security principal name (SPN)**: The name that identifies a security principal (for example, machinename\$@domainname for a machine joined to a domain or username@domainname for a user). Domainname is resolved using the Domain Name System (DNS).

**Server Message Block (SMB)**: A protocol that is used to request file and print services from server systems over a network. The SMB protocol extends the CIFS protocol with additional security, file, and disk management support. For more information, see [\[CIFS\]](http://go.microsoft.com/fwlink/?LinkId=89836) and [\[MS-SMB\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SMB%5d.pdf#Section_f210069c70864dc2885e861d837df688).

**service principal name (SPN)**: The name a client uses to identify a service for mutual authentication. (For more information, see [\[RFC1964\]](http://go.microsoft.com/fwlink/?LinkId=90304) section 2.1.1.) An [**SPN**](#gt_547217ca-134f-4b43-b375-f5bca4c16ce4) consists of either two parts or three parts, each separated by a forward slash ('/'). The first part is the service class, the second part is the instance name, and the third part (if present) is the service name. For example, "ldap/dc-01.fabrikam.com/fabrikam.com" is a three-part [**SPN**](#gt_547217ca-134f-4b43-b375-f5bca4c16ce4) where "ldap" is the service class name, "dc-01.fabrikam.com" is the instance name, and "fabrikam.com" is the service name. See [\[SPNNAMES\]](http://go.microsoft.com/fwlink/?LinkId=90532) for more information about [**SPN**](#gt_547217ca-134f-4b43-b375-f5bca4c16ce4) format and composing a unique [**SPN**](#gt_547217ca-134f-4b43-b375-f5bca4c16ce4).

**session**: In [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625), a persistent-state association between an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client and [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server. A [**session**](#gt_0cd96b80-a737-4f06-bca4-cf9efb449d12) is tied to the lifetime of the underlying NetBIOS or TCP connection.

**shadow copy**: A duplicate of data held on a volume at a well-defined instant in time.

**share**: A resource offered by a Common Internet File System (CIFS) server for access by CIFS clients over the network. A [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3) typically represents a directory tree and its included files (referred to commonly as a "disk share" or "file share") or a printer (a "print share"). If the information about the [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3) is saved in persistent store (for example, Windows registry) and reloaded when a file server is restarted, then the [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3) is referred to as a "sticky share". Some [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3) names are reserved for specific functions and are referred to as special [**shares**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3): IPC\$, reserved for interprocess communication, ADMIN\$, reserved for remote administration, and A\$, B\$, C\$ (and other local disk names followed by a dollar sign), assigned to local disk devices.

**share connect**: The act of establishing authentication and shared state between a Common Internet File System (CIFS) server and client that allows a CIFS client to access a [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3) offered by the CIFS server.

**SMB command**: A set of SMB messages that are exchanged in order to perform an operation. An [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) command is typically identified by a unique command code in the message headers, although some [**SMB commands**](#gt_dbae2612-173d-4988-9301-86cb559029f9) require the use of secondary commands. Within \[MS-CIFS\], the term command means an [**SMB command**](#gt_dbae2612-173d-4988-9301-86cb559029f9) unless otherwise stated.

**SMB connection**: A transport connection between a [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client and an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server. The [**SMB connection**](#gt_e1d88514-18e6-4e2e-a459-20d5e17e9078) is assumed to provide reliable in-order message delivery semantics. An [**SMB connection**](#gt_e1d88514-18e6-4e2e-a459-20d5e17e9078) can be established over any available [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) transport that is supported by both the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client and the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server, as specified in \[MS-CIFS\].

**SMB dialect**: There are several different versions and subversions of the [**Server Message Block (SMB)**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) protocol. A particular version of the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) protocol is referred to as an [**SMB dialect**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1). Different [**SMB dialects**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1) can include both new [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) messages as well as changes to the fields and semantics of existing [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) messages used in other [**SMB dialects**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1). When an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client connects to an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server, the client and server negotiate the [**SMB dialect**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1) to be used.

**SMB message**: A protocol data unit. [**SMB messages**](#gt_fb37399e-d72d-41b6-b073-086da2d96e09) are comprised of a header, a parameter section, and a data section. The latter two can be zero length. An [**SMB message**](#gt_1308cf27-6aba-4d86-b38d-7926ba662311) is sometimes referred to simply as an SMB. Within \[MS-CIFS\], the term command means an [**SMB command**](#gt_dbae2612-173d-4988-9301-86cb559029f9) unless otherwise stated.

**SMB session**: An authenticated user connection established between an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) client and an [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) server over an [**SMB connection**](#gt_e1d88514-18e6-4e2e-a459-20d5e17e9078). There can be multiple active [**SMB sessions**](#gt_ee1ec898-536f-41c4-9d90-b4f7d981fd67) over a single [**SMB connection**](#gt_e1d88514-18e6-4e2e-a459-20d5e17e9078). The Uid field in the [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625) packet header distinguishes the various sessions.

**snapshot**: The point in time at which a [**shadow copy**](#gt_34537940-5a56-4122-b6ff-b9a4d065d066) of a volume is made.

**stream**: A sequence of bytes written to a file on the [**NTFS**](#gt_86f79a17-c0be-4937-8660-0cf6ce5ddc1a) file system. Every file stored on a volume that uses the [**NTFS**](#gt_86f79a17-c0be-4937-8660-0cf6ce5ddc1a) file system contains at least one [**stream**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6), which is normally used to store the primary contents of the file. Additional [**streams**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6) within the file can be used to store file attributes, application parameters, or other information specific to that file. Every file has a default data [**stream**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6), which is unnamed by default. That data [**stream**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6), and any other data [**stream**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6) associated with a file, can optionally be named.

**system access control list (SACL)**: An access control list (ACL) that controls the generation of audit messages for attempts to access a securable object. The ability to get or set an object's [**SACL**](#gt_c189801e-3752-4715-88f4-17804dad5782) is controlled by a privilege typically held only by system administrators.

**Transmission Control Protocol (TCP)**: A protocol used with the Internet Protocol (IP) to send data in the form of message units between computers over the Internet. TCP handles keeping track of the individual units of data (called packets) that a message is divided into for efficient routing through the Internet.

**tree connect**: A connection between a CIFS client and a share on a remote CIFS server.

**Unicode**: A character encoding standard developed by the Unicode Consortium that represents almost all of the written languages of the world. The [**Unicode**](#gt_c305d0ab-8b94-461a-bd76-13b40cb8c4d8) standard [\[UNICODE5.0.0/2007\]](http://go.microsoft.com/fwlink/?LinkId=154659) provides three forms (UTF-8, UTF-16, and UTF-32) and seven schemes (UTF-8, UTF-16, UTF-16 BE, UTF-16 LE, UTF-32, UTF-32 LE, and UTF-32 BE).

**Unicode string**: A [**Unicode**](#gt_c305d0ab-8b94-461a-bd76-13b40cb8c4d8) 8-bit string is an ordered sequence of 8-bit units, a [**Unicode**](#gt_c305d0ab-8b94-461a-bd76-13b40cb8c4d8) 16-bit string is an ordered sequence of 16-bit code units, and a [**Unicode**](#gt_c305d0ab-8b94-461a-bd76-13b40cb8c4d8) 32-bit string is an ordered sequence of 32-bit code units. In some cases, it could be acceptable not to terminate with a terminating null character. Unless otherwise specified, all [**Unicode strings**](#gt_b069acb4-e364-453e-ac83-42d469bb339e) follow the UTF-16LE encoding scheme with no Byte Order Mark (BOM).

**volume identifier (VolumeId)**: A 128-bit value used to represent a volume. The value of a [**VolumeId**](#gt_892a6724-e635-4ba0-8b8a-d6368f166221) is unique on a single computer (the local file system or a remote file server).

**MAY, SHOULD, MUST, SHOULD NOT, MUST NOT:** These terms (in all caps) are used as defined in [\[RFC2119\]](http://go.microsoft.com/fwlink/?LinkId=90317). All statements of optional behavior use either MAY, SHOULD, or SHOULD NOT.

## References

Links to a document in the Microsoft Open Specifications library point to the correct section in the most recently published version of the referenced document. However, because individual documents in the library are not updated at the same time, the section numbers in the documents may not match. You can confirm the correct section numbering by checking the [Errata](http://msdn.microsoft.com/en-us/library/dn781092.aspx).

### Normative References

We conduct frequent surveys of the normative references to assure their continued availability. If you have any issue with finding a normative reference, please contact [dochelp@microsoft.com](mailto:dochelp@microsoft.com). We will assist you in finding the relevant information.

\[IANAPORT\] IANA, "Service Name and Transport Protocol Port Number Registry", November 2006, [http://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml](http://go.microsoft.com/fwlink/?LinkId=89888)

\[MS-CIFS\] Microsoft Corporation, "[Common Internet File System (CIFS) Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b)".

\[MS-DFSC\] Microsoft Corporation, "[Distributed File System (DFS): Referral Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSC%5d.pdf#Section_3109f4be2dbb42c99b8e0b34f7a2135e)".

\[MS-DTYP\] Microsoft Corporation, "[Windows Data Types](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2)".

\[MS-EFSR\] Microsoft Corporation, "[Encrypting File System Remote (EFSRPC) Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-EFSR%5d.pdf#Section_08796ba801c8487292211000ec2eff31)".

\[MS-FSA\] Microsoft Corporation, "[File System Algorithms](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSA%5d.pdf#Section_860b1516c45247b4bdbc625d344e2041)".

\[MS-FSCC\] Microsoft Corporation, "[File System Control Codes](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e)".

\[MS-KILE\] Microsoft Corporation, "[Kerberos Protocol Extensions](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9)".

\[MS-NLMP\] Microsoft Corporation, "[NT LAN Manager (NTLM) Authentication Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-NLMP%5d.pdf#Section_b38c36ed28044868a9ff8dd3182128e4)".

\[MS-RAP\] Microsoft Corporation, "[Remote Administration Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-RAP%5d.pdf#Section_fb8d5bd1e57c4be1b063ec31330bdd58)".

\[MS-SRVS\] Microsoft Corporation, "[Server Service Remote Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SRVS%5d.pdf#Section_accf23b00f57441c918543041f1b0ee9)".

\[RFC1321\] Rivest, R., "The MD5 Message-Digest Algorithm", RFC 1321, April 1992, [http://www.ietf.org/rfc/rfc1321.txt](http://go.microsoft.com/fwlink/?LinkId=90275)

\[RFC2104\] Krawczyk, H., Bellare, M., and Canetti, R., "HMAC: Keyed-Hashing for Message Authentication", RFC 2104, February 1997, [http://www.ietf.org/rfc/rfc2104.txt](http://go.microsoft.com/fwlink/?LinkId=90314)

\[RFC2119\] Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997, [http://www.rfc-editor.org/rfc/rfc2119.txt](http://go.microsoft.com/fwlink/?LinkId=90317)

\[RFC2743\] Linn, J., "Generic Security Service Application Program Interface Version 2, Update 1", RFC 2743, January 2000, [http://www.rfc-editor.org/rfc/rfc2743.txt](http://go.microsoft.com/fwlink/?LinkId=90378)

\[RFC4178\] Zhu, L., Leach, P., Jaganathan, K., and Ingersoll, W., "The Simple and Protected Generic Security Service Application Program Interface (GSS-API) Negotiation Mechanism", RFC 4178, October 2005, [http://www.rfc-editor.org/rfc/rfc4178.txt](http://go.microsoft.com/fwlink/?LinkId=90461)

### Informative References

\[MD5Collision\] Klima, V., "Tunnels in Hash Functions: MD5 Collisions Within a Minute", March 2006, [http://eprint.iacr.org/2006/105.pdf](http://go.microsoft.com/fwlink/?LinkId=89937)

\[MS-AUTHSOD\] Microsoft Corporation, "[Authentication Services Protocols Overview](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AUTHSOD%5d.pdf#Section_953d700a57cb4cf7b0c3a64f34581cc9)".

\[MS-BRWSA\] Microsoft Corporation, "[Common Internet File System (CIFS) Browser Auxiliary Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-BRWSA%5d.pdf#Section_5995d2f2fff140af9100ca67794d50a5)".

\[MS-BRWS\] Microsoft Corporation, "[Common Internet File System (CIFS) Browser Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-BRWS%5d.pdf#Section_d2d83b294b62479eb4279b750303387b)".

\[MS-DFSNM\] Microsoft Corporation, "[Distributed File System (DFS): Namespace Management Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSNM%5d.pdf#Section_95a506a8cae64c42b19d9c1ed1223979)".

\[MS-ERREF\] Microsoft Corporation, "[Windows Error Codes](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-ERREF%5d.pdf#Section_1bc92ddfb79e413cbbaa99a5281a6c90)".

\[MS-MAIL\] Microsoft Corporation, "[Remote Mailslot Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-MAIL%5d.pdf#Section_8ea19aa46e5a4aedb6280b5cd75a1ab9)".

\[MS-RPCE\] Microsoft Corporation, "[Remote Procedure Call Protocol Extensions](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-RPCE%5d.pdf#Section_290c38b192fe422991e64fc376610c15)".

\[MS-SMB2\] Microsoft Corporation, "[Server Message Block (SMB) Protocol Versions 2 and 3](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SMB2%5d.pdf#Section_5606ad475ee0437a817e70c366052962)".

\[MS-WKST\] Microsoft Corporation, "[Workstation Service Remote Protocol](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-WKST%5d.pdf#Section_5bb08058bc364d3cabebb132228281b7)".

\[MS-WPO\] Microsoft Corporation, "[Windows Protocols Overview](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-WPO%5d.pdf#Section_c5f54a7765be40a0bb829e4181d8ab67)".

\[MSBRWSE\] Thompson IV, D. and McLaughlin, R., "MS Windows NT Browser", [https://www.microsoft.com/technet/archive/winntas/deploy/prodspecs/ntbrowse.mspx](http://go.microsoft.com/fwlink/?LinkId=89943)

\[MSDFS\] Microsoft Corporation, "How DFS Works", March 2003, [http://technet.microsoft.com/en-us/library/cc782417%28WS.10%29.aspx](http://go.microsoft.com/fwlink/?LinkId=89945)

\[MSDN-IMPERS\] Microsoft Corporation, "Impersonation", [http://msdn.microsoft.com/en-us/library/ms691341.aspx](http://go.microsoft.com/fwlink/?LinkId=106009)

\[MSKB-121007\] Microsoft Corporation, "Long Name: How to Disable the 8.3 Name Creation on NTFS Partitions", December 2007, [http://support.microsoft.com/kb/121007](http://go.microsoft.com/fwlink/?LinkId=228457)

\[NETBEUI\] IBM Corporation, "LAN Technical Reference: 802.2 and NetBIOS APIs", 1986, [http://publibz.boulder.ibm.com/cgi-bin/bookmgr_OS390/BOOKS/BK8P7001/CCONTENTS](http://go.microsoft.com/fwlink/?LinkId=90224)

\[RFC1001\] Network Working Group, "Protocol Standard for a NetBIOS Service on a TCP/UDP Transport: Concepts and Methods", RFC 1001, March 1987, [http://www.ietf.org/rfc/rfc1001.txt](http://go.microsoft.com/fwlink/?LinkId=90260)

\[RFC1002\] Network Working Group, "Protocol Standard for a NetBIOS Service on a TCP/UDP Transport: Detailed Specifications", STD 19, RFC 1002, March 1987, [http://www.rfc-editor.org/rfc/rfc1002.txt](http://go.microsoft.com/fwlink/?LinkId=90261)

\[RFC793\] Postel, J., Ed., "Transmission Control Protocol: DARPA Internet Program Protocol Specification", RFC 793, September 1981, [http://www.rfc-editor.org/rfc/rfc793.txt](http://go.microsoft.com/fwlink/?LinkId=150872)

\[SNIA\] Storage Networking Industry Association, "Common Internet File System (CIFS) Technical Reference, Revision 1.0", March 2002, [http://networks.cs.ucdavis.edu/~zhuk/research/sans/documents/CIFS-TR-1p00_FINAL.pdf](http://go.microsoft.com/fwlink/?LinkId=90519)

## Overview

Client systems use the Common Internet File System (CIFS) Protocol to request file and print services from server systems over a network. CIFS is a stateful protocol, in which clients establish a [**session**](#gt_0cd96b80-a737-4f06-bca4-cf9efb449d12) with a server and use that session to make a variety of requests to access files, printers, and inter-process communication (IPC) mechanisms, such as [**named pipes**](#gt_34f1dfa8-b1df-4d77-aa6e-d777422f9dca). CIFS imposes state to maintain an authentication context, cryptographic operations, file semantics, such as locking, and similar features. A detailed overview of how the CIFS Protocol functions is provided in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.

The Server Message Block (SMB) Version 1.0 Protocol extends the CIFS Protocol with additional security, file, and disk management support. These extensions do not alter the basic message sequencing of the CIFS Protocol but introduce new flags, extended requests and responses, and new [**Information Levels**](#gt_b01da706-86d0-4ee2-9461-2d9fb1060543). All of these extensions follow a request/response pattern in which the client initiates all of the requests. The base protocol allows for one exception to this pattern--[**oplock breaks**](#gt_5c86b468-90a1-4542-8bde-460c098d2a5a)\--as specified in \[MS-CIFS\] section 3.2.5.42.

This document defines the SMB Version 1.0 Protocol extensions to CIFS, which provide support for the following features:

- New authentication methods, including Kerberos. The Negotiate and Session Setup commands have been enhanced to carry opaque security tokens to support mechanisms that are compatible with the Generic Security Services (GSS).
- Enumeration and access to previous versions of files. A new subcommand that uses a file system control ([**FSCTL**](#gt_4ffb96a7-5fad-488e-9438-b7707d2e4226)) allows the client to query the server for the presence of older versions of files. If the server implements a file system with versioning, then this can be exposed to clients.
- Client requests for server-side data movement operations between files without requiring the data to be read by the client and then written back to the server. As specified in \[MS-CIFS\], to copy a file on the server requires the client to read all of the data from the server and then write the data back to the server. The SMB Version 1.0 Protocol introduces a method by which such an operation can be done entirely on the server without consuming network resources.
- [**SMB connections**](#gt_e1d88514-18e6-4e2e-a459-20d5e17e9078) that use Direct TCP for the SMB transport. The CIFS Protocol supports the use of NBT for connections, as specified in \[MS-CIFS\] section 2.1.1.2. The SMB Version 1.0 Protocol includes a method to connect directly over [**TCP**](#gt_b08d36f6-b5c6-4ce4-8d2d-6f2ab75ea4cb) (see [\[RFC793\]](http://go.microsoft.com/fwlink/?LinkId=150872)) without involving NetBIOS (see [\[RFC1001\]](http://go.microsoft.com/fwlink/?LinkId=90260) and [\[RFC1002\]](http://go.microsoft.com/fwlink/?LinkId=90261)). Information about NetBIOS is specified in [\[NETBEUI\]](http://go.microsoft.com/fwlink/?LinkId=90224).
- Support for retrieving extended information in response to [**share connect**](#gt_956367c4-c5cb-49a4-bf4b-9bd1596ed5d0) and file open operations. Certain server functionality and indicators (such as the need for the client to cache the contents of a [**share**](#gt_a49a79ea-dac7-4016-9a84-cf87161db7e3)) are new in the SMB Version 1.0 Protocol and are returned to the client through these extensions to existing commands.
- Additional [**SMB commands**](#gt_dbae2612-173d-4988-9301-86cb559029f9) for the setting and querying of quotas by user. Provided the server supports quotas, the client can constrain the file system capacity consumed by the files of users.

Many of these capabilities are exposed in enhancements to the [SMB_COM_NEGOTIATE](#Section_9fef5150550144a8b5b50b20057187ac) (section 2.2.4.5) and [SMB_COM_SESSION_SETUP_ANDX](#Section_115b551adcd74ff28c59a334b92e01c0) (section 2.2.4.6) command requests and responses.

## Relationship to Other Protocols

The extensions to the CIFS protocol rely on the Simple and Protected Generic Security Service Application Program Interface Negotiation Mechanism (SPNEGO), as described in [\[MS-AUTHSOD\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AUTHSOD%5d.pdf#Section_953d700a57cb4cf7b0c3a64f34581cc9) section 2.1.2.3.1 and specified in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461), for authentication, which in turn relies on Kerberos, as specified in [\[MS-KILE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9), and/or the NT LAN Manager (NTLM), as specified in [\[MS-NLMP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-NLMP%5d.pdf#Section_b38c36ed28044868a9ff8dd3182128e4), challenge/response authentication protocol.

The Server Message Block (SMB) Version 2 Protocol is a new version of [**SMB**](#gt_09dbec39-5e75-4d9a-babf-1c9f1d499625). For more information about the SMB Version 2 Protocol, see [\[MS-SMB2\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SMB2%5d.pdf#Section_5606ad475ee0437a817e70c366052962). This specification does not require implementation of the SMB Version 2 Protocol.

The following protocols extend this specification to provide additional functionality:

- The Distributed File System (DFS): Namespace Referral Protocol, as specified in [\[MS-DFSC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSC%5d.pdf#Section_3109f4be2dbb42c99b8e0b34f7a2135e). For more information, see [\[MSDFS\]](http://go.microsoft.com/fwlink/?LinkId=89945). For management of [**DFS**](#gt_0b8086c9-d025-45b8-bf09-6b5eca72713e), see [\[MS-DFSNM\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSNM%5d.pdf#Section_95a506a8cae64c42b19d9c1ed1223979).

The following protocols can use the SMB Version 1.0 Protocol as a transport:

- - The Remote Procedure Call (RPC) Protocol Extensions. Note that when named pipes are used, this protocol requires the SMB Protocol. For more information, see [\[MS-RPCE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-RPCE%5d.pdf#Section_290c38b192fe422991e64fc376610c15).
    - The Remote Mailslot Protocol. This protocol can use the SMB Version 1.0 Protocol as a transport but supports other transports as well. For more information, see [\[MS-MAIL\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-MAIL%5d.pdf#Section_8ea19aa46e5a4aedb6280b5cd75a1ab9).
    - The CIFS Browser Protocol. This protocol uses the Remote Mailslot Protocol and the RAP as transport protocols, which in turn can use this specification. It does not use this specification directly, but is included here for completeness. For more information, see [\[MS-BRWS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-BRWS%5d.pdf#Section_d2d83b294b62479eb4279b750303387b) and [\[MSBRWSE\]](http://go.microsoft.com/fwlink/?LinkId=89943).

The SMB protocol server, upon request from an underlying object store, optionally invokes the Encrypting File System Remote (EFSRPC) protocol when a user attempts to open or create a new encrypted file. For more information, see [\[MS-FSA\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSA%5d.pdf#Section_860b1516c45247b4bdbc625d344e2041) and [\[MS-EFSR\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-EFSR%5d.pdf#Section_08796ba801c8487292211000ec2eff31).

For more information, see [\[MS-BRWSA\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-BRWSA%5d.pdf#Section_5995d2f2fff140af9100ca67794d50a5) and [\[MS-WKST\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-WKST%5d.pdf#Section_5bb08058bc364d3cabebb132228281b7).

The following diagram illustrates the relationship amongst the protocols.

![Relationships to other protocols](data:image/png;base64,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)

Figure 1: Relationships to other protocols

## Prerequisites/Preconditions

The SMB Version 1.0 Protocol assumes the availability of the following resources:

- An underlying transport protocol that supports reliable, in-order message delivery.
- An underlying object store on the server, such as a file system, exposing file, named pipe, or printer objects.

## Applicability Statement

The extensions specified in this document are applicable to environments in which the security characteristics of the base protocol, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b), are insufficient. In particular, these extensions provide for enhanced message integrity and stronger authentication mechanisms.

The extensions are applicable to an environment that requires tighter data retention policies. In particular, through the use of previous version capabilities, the extensions allow access to versions of a file that have been changed or deleted when the server supports this capability. This feature is applicable to environments that require more stringent data retention policies that include maintaining access to previous versions of files.

## Versioning and Capability Negotiation

This document covers versioning issues in the following areas:

- Supported Transports: The extensions in this document add additional transports, as defined in section [2.1](#Section_f906c680330c43ae9a71f854e24aeee6).
- Security and Authentication Methods: The extensions in this document add additional authentication methods, as specified in section [3.2.4.2](#Section_061aa811d5e044c3974243ae0a222a59).
- Capability Negotiation: The extensions in this document use capability negotiation, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) sections 1.7, 2.2.4.52, 2.2.4.53, and 3.3.1.2.

[**SMB dialect**](#gt_29b8d978-7450-42b5-845c-045fc72b0fe1) negotiation is handled as specified in \[MS-CIFS\] sections 1.7 and 3.2.4.2. The extensions specified in this document introduce no new dialects and apply only to connections that have negotiated the **NT LAN Manager** dialect, as identified by the "NT LM 0.12" dialect identification string. The extensions specified in this document are detected via the following methods:

- They can be returned in the **Capabilities** field, as specified in \[MS-CIFS\] section 2.2.4.52. Specific new capability options are defined in this document.
- They can be supplied or returned in the **Flags** and **Flags2** fields of the SMB header, as specified in \[MS-CIFS\] sections 2.2.3.1.
- A server can return an error code (STATUS_NOT_SUPPORTED) when a client request is sent to a server for a new feature that is not supported.

A client written to support these extensions cannot require that the target server implement these extensions to successfully connect. Thus, a server that does not implement an extension is still accessible by a client that implements that extension, although the relevant new features might not be available. The one exception is that a client offers the capability to be configured to require the new security features to create a more secure environment so that the client could be restricted from connecting successfully to servers that do not implement these features.

Negotiation of the use of the Generic Security Service Application Program Interface (GSS API) for authentication is specified in section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953). The GSS API is specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378).

## Vendor-Extensible Fields

The CAP_UNIX capability bit is specified in order to allow third-party implementers to collaborate on the definition of a specific set of extensions. SMB_COM_TRANSACTION2 Information Levels in the range 0x200 to 0x3E0 (inclusive) are reserved for these extensions.[&lt;1&gt;](#Appendix_A_1)

## Standards Assignments

In addition to any standards assignments specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b), the Direct TCP Transport, as specified in section [2.2](#Section_6cdbc7263e4a499982b3f5e3d7f3c37a), makes use of the following assignment:

| Parameter    | TCP port value | Reference                                                    |
| ------------ | -------------- | ------------------------------------------------------------ |
| Microsoft-DS | 445 (0x01BD)   | [\[IANAPORT\]](http://go.microsoft.com/fwlink/?LinkId=89888) |

**SMB** transports can have assigned port numbers or other assigned values. See the documentation for the specific transport for more information.

# Messages

An SMB Version 1.0 Protocol implementation MUST implement CIFS, as specified by section 2 of the [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) specification.

## Transport

In addition to the transport protocols listed in section 2.1 of [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b), the extended version of the protocol supports the use of TCP as a transport layer. Hereafter, the special TCP-related characteristics that are employed in the application of SMB over TCP are known as the Direct TCP transport.[&lt;2&gt;](#Appendix_A_2)

The extended version of the SMB Version 1.0 Protocol can use Direct TCP over either IPv4 or IPv6 as a reliable stream-oriented transport for [**SMB messages**](#gt_fb37399e-d72d-41b6-b073-086da2d96e09). No NetBIOS layer is provided or used. TCP provides a full, duplex, sequenced, and reliable transport for the connection. When using TCP as the reliable connection-oriented transport, the extended version of the SMB Version 1.0 Protocol makes no higher-level attempts to ensure sequenced delivery of messages between a client and server. The TCP transport has mechanisms to detect failures of either the client node or the server node, and to deliver such an indication to the client or server software so that it can clean up the state.

When using Direct TCP as the SMB transport, the implementer MUST establish a TCP connection from an SMB client to a TCP port on the server. The TCP source port used by the SMB client can be of any TCP port value. The SMB server SHOULD listen for connections on port 445. This port number has been registered with the Internet Assigned Numbers Authority (IANA) and has been officially assigned for Microsoft-DS.[&lt;3&gt;](#Appendix_A_3)

When using Direct TCP as the SMB transport, the implementer MUST prepend a 4-byte Direct TCP transport packet header to each [**SMB message**](#gt_1308cf27-6aba-4d86-b38d-7926ba662311). This transport header MUST be formatted as a byte of zero (8 zero bits) followed by 3 bytes that indicate the length of the SMB message that is encapsulated. The body of the SMB packet follows as a variable-length payload. A Direct TCP transport packet has the following structure (in [**network byte order**](#gt_502de58c-ffc0-4dda-8fcb-b152b2c31fba)).

| 0                      | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                      | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ---------------------- | --- | --- | --- | --- | --- | --- | --- | ---------------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Zero                   |     |     |     |     |     |     |     | Stream Protocol Length |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SMB Message (variable) |     |     |     |     |     |     |     |                        |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                    |     |     |     |     |     |     |     |                        |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Zero (1 byte):** The first byte of the Direct TCP transport packet header MUST be zero (0x00).

**Stream Protocol Length (3 bytes):** The length, in bytes, of the SMB message. This length is formatted as a 3-byte integer in network byte order. The length field does not include the 4-byte Direct TCP transport header; rather, it is only the length of the enclosed SMB message. For SMB messages, if this value exceeds 0x1FFFF, the server SHOULD[&lt;4&gt;](#Appendix_A_4) disconnect the connection.

**SMB Message (variable):** The body of the SMB packet. The length of an SMB message varies based on the SMB command represented by the message.

## Message Syntax

A client exchanges messages with a server to access resources on the server. These messages are called SMB messages or SMBs. Every SMB message has a common format, as defined in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.

All SMB messages MUST begin with a fixed-length SMB header (as specified in \[MS-CIFS\], section 2.2.1). The header contains a command field that indicates the operation code that the client requests or to which the server responds. An SMB message is of variable length. The actual length depends on the SMB command field (and consequent appended data structures) and whether the SMB message is a client request or a server response.

Unless otherwise indicated, numeric fields are integers of the specified byte length.

Unless otherwise specified, multibyte fields (that is, 16-bit, 32-bit, and 64-bit fields) in an SMB message MUST be transmitted in [**little-endian**](#gt_079478cb-f4c5-4ce5-b72b-2144da5d2ce7) byte order (least significant byte first).

Unless otherwise noted, fields marked as Reserved SHOULD be set to zero when being sent and MUST be ignored upon receipt. Unless otherwise noted, unused or reserved bits in bit fields SHOULD be set to zero when being sent and MUST be ignored upon receipt.

When an error occurs, unless otherwise noted in this specification, an SMB server SHOULD return an Error Response message. An Error Response message is comprised of a complete SMB header, along with an empty parameter and data portion.[&lt;5&gt;](#Appendix_A_5)

### Common Data Type Extensions

#### Character Sequences

##### Pathname Extensions

In addition to the specification in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.1.1.2, pathnames include the following extension:

- Previous Version Tokens -- Support for this feature is optional.[&lt;6&gt;](#Appendix_A_6)

Pathnames are allowed to contain a previous version token (or [**@GMT token**](#gt_9fb4d987-6d9a-4ef5-81be-eeae393afb40)), as a directory element in a path. A previous version token indicates that the pathname is a request to access the previous version (or [**shadow copy**](#gt_34537940-5a56-4122-b6ff-b9a4d065d066)) of the file or directory at a particular point in time. This feature is available on any path-based operation (for example, SMB_COM_NT_CREATE_ANDX). A pathname MUST NOT contain more than one previous version token.

For example, requesting a previous version of the file \\\\server\\mydocs\\reviews\\feb01.doc at 2:44:00 P.M. on March30, 2001 [**UTC**](#gt_f2369991-a884-4843-a8fa-1505b6d5ece7) is specified in the following format:

- \\\\server\\mydocs\\reviews\\@GMT-2001.03.30-14.44.00\\feb01.doc

The same technique can be used to build a path that represents a previous version of a directory as opposed to a file.

For example, requesting a previous version of the directory \\\\server\\mydocs\\reviews at 2:44:00 PM on 3/30/01 UTC can be specified in either of the following formats:

A token appearing as an intermediate path component:

- \\\\server\\mydocs\\@GMT-2001.03.30-14.44.00\\reviews

A token appearing as a final path component:

- \\\\server\\mydocs\\reviews\\@GMT-2001.03.30-14.44.00

In addition, it is possible to request an enumeration of available previous version timestamps (or [**snapshots**](#gt_24e415c9-f158-4de0-b687-598511501c68)) of a file or directory. While the NT_TRANSACT_IOCTL subcommand can be used with the FSCTL_SRV_ENUMERATE_SNAPSHOTS FSCTL code to enumerate available previous version timestamps using a valid [**Fid**](#gt_ab858f4d-7f0c-474c-9697-50f0af92f766) (section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17)), these extensions also present a path-based method to access this functionality. The TRANS2_FIND_FIRST2 subcommand's SMB_FIND_FILE_BOTH_DIRECTORY_INFO Information Level (section [2.2.6.1](#Section_aedac660ae304dc186ce160c223fc883)) has been extended to allow a special previous version wildcard token, @GMT-\*.

For example, requesting an enumeration of available previous version timestamps of the examples, discussed earlier in this section, can be specified in the following ways:

- \\\\server\\mydocs\\reviews\\@GMT-\*\\feb01.doc
- \\\\server\\mydocs\\@GMT-\*\\reviews
- \\\\server\\mydocs\\reviews\\@GMT-\*

#### File Attributes

##### Extended File Attribute (SMB_EXT_FILE_ATTR) Extensions

The list of extended file attributes valid in 32-bit attribute values, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.1.2.3, has been extended to include the following attributes:

- ATTR_SPARSE
- ATTR_REPARSE_POINT
- ATTR_OFFLINE
- ATTR_NOT_CONTENT_INDEXED
- ATTR_ENCRYPTED

The following table lists all possible values. Unless otherwise noted, any combination of these values is acceptable.

| Name & bitmask                             | Extension | Meaning                                                                                                                                                                                               |
| ------------------------------------------ | --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ATTR_READONLY<br><br>0x00000001            | No        | File is read-only. Applications cannot write or delete the file.                                                                                                                                      |
| ATTR_HIDDEN<br><br>0x00000002              | No        | File is hidden. It is not to be included in an ordinary directory enumeration.                                                                                                                        |
| ATTR_SYSTEM<br><br>0x00000004              | No        | File is part of or is used exclusively by the operating system.                                                                                                                                       |
| ATTR_DIRECTORY<br><br>0x00000010           | No        | File is a directory.                                                                                                                                                                                  |
| ATTR_ARCHIVE<br><br>0x00000020             | No        | File has not been archived since it was last modified.                                                                                                                                                |
| ATTR_NORMAL<br><br>0x00000080              | No        | File has no other attributes set. This value is valid only when used alone.                                                                                                                           |
| ATTR_TEMPORARY<br><br>0x00000100           | No        | File is temporary.                                                                                                                                                                                    |
| ATTR_SPARSE<br><br>0x00000200              | Yes       | File is a sparse file.                                                                                                                                                                                |
| ATTR_REPARSE_POINT<br><br>0x00000400       | Yes       | File or directory has an associated [**reparse point**](#gt_4fed0b53-5fc8-4818-886f-93d87f3035e1).                                                                                                    |
| ATTR_COMPRESSED<br><br>0x00000800          | No        | File is compressed on the disk. This does not affect how it is transferred over the network.                                                                                                          |
| ATTR_OFFLINE<br><br>0x00001000             | Yes       | File data is not available. The attribute indicates that the file has been moved to offline storage.                                                                                                  |
| ATTR_NOT_CONTENT_INDEXED<br><br>0x00002000 | Yes       | File or directory SHOULD NOT be indexed by a content indexing service.                                                                                                                                |
| ATTR_ENCRYPTED<br><br>0x00004000           | Yes       | File or directory is encrypted. For a file, this means that all data in the file is encrypted. For a directory, this means that encryption is the default for newly created files and subdirectories. |
| Reserved<br><br>0xFFFF8048                 | N/A       | SHOULD be set to zero when sending and MUST be ignored upon receipt of the message.                                                                                                                   |

##### File System Attribute Extensions

The list of file system attributes, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.8.2.6, has been extended. For completeness, the following table lists all of the available attribute flags and their symbolic constants. Unless otherwise noted, any combination of the following bits is valid. Any bit that is not listed in this section is considered reserved; the sender SHOULD set it to zero, and the receiver MUST ignore it. For more information, see [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.5.1.

| Name & bitmask                                      | Extension                       | Meaning                                                                                                                                                                    |
| --------------------------------------------------- | ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FILE_CASE_SENSITIVE_SEARCH<br><br>0x00000001        | No                              | File system supports case-sensitive file names.                                                                                                                            |
| FILE_CASE_PRESERVED_NAMES<br><br>0x00000002         | No                              | File system preserves the case of file names when it stores the name on disk.                                                                                              |
| FILE_UNICODE_ON_DISK<br><br>0x00000004              | No                              | File system supports [**Unicode**](#gt_c305d0ab-8b94-461a-bd76-13b40cb8c4d8) in file names.                                                                                |
| FILE_PERSISTENT_ACLS<br><br>0x00000008              | No                              | File system preserves and enforces access control lists.                                                                                                                   |
| FILE_FILE_COMPRESSION<br><br>0x00000010             | No                              | File system supports file-based compression. This flag is incompatible with FILE_VOLUME_IS_COMPRESSED. This flag does not affect how data is transferred over the network. |
| FILE_VOLUME_QUOTAS<br><br>0x00000020                | Yes                             | File system supports per-user quotas.                                                                                                                                      |
| FILE_SUPPORTS_SPARSE_FILES<br><br>0x00000040        | Yes                             | File system supports sparse files.                                                                                                                                         |
| FILE_SUPPORTS_REPARSE_POINTS<br><br>0x00000080      | Yes                             | File system supports reparse points.                                                                                                                                       |
| FILE_SUPPORTS_REMOTE_STORAGE<br><br>0x00000100      | Yes                             | File system supports remote storage.                                                                                                                                       |
| FILE_VOLUME_IS_COMPRESSED<br><br>0x00008000         | No                              | Volume is a compressed volume. This flag is incompatible with FILE_FILE_COMPRESSION. This does not affect how data is transferred over the network.                        |
| FILE_SUPPORTS_OBJECT_IDS<br><br>0x00010000          | Yes                             | File system supports object identifiers.                                                                                                                                   |
| FILE_SUPPORTS_ENCRYPTION<br><br>0x00020000          | Yes                             | File system supports encryption.                                                                                                                                           |
| FILE_NAMED_STREAMS<br><br>0x00040000                | Yes                             | File system supports multiple named data [**streams**](#gt_f3529cd8-50da-4f36-aa0b-66af455edbb6) for a file.                                                               |
| FILE_READ_ONLY_VOLUME<br><br>0x00080000             | Yes[&lt;7&gt;](#Appendix_A_7)   | Specified volume is read-only.                                                                                                                                             |
| FILE_SEQUENTIAL_WRITE_ONCE<br><br>0x00100000        | Yes[&lt;8&gt;](#Appendix_A_8)   | Specified volume can be written to one time only. The write MUST be performed in sequential order.                                                                         |
| FILE_SUPPORTS_TRANSACTIONS<br><br>0x00200000        | Yes[&lt;9&gt;](#Appendix_A_9)   | File system supports transaction processing.                                                                                                                               |
| FILE_SUPPORTS_HARD_LINKS<br><br>0x00400000          | Yes[&lt;10&gt;](#Appendix_A_10) | File system supports direct links to other devices and partitions.                                                                                                         |
| FILE_SUPPORTS_EXTENDED_ATTRIBUTES<br><br>0x00800000 | Yes[&lt;11&gt;](#Appendix_A_11) | File system supports extended attributes (EAs).                                                                                                                            |
| FILE_SUPPORTS_OPEN_BY_FILE_ID<br><br>0x01000000     | Yes[&lt;12&gt;](#Appendix_A_12) | File system supports open by FileID.                                                                                                                                       |
| FILE_SUPPORTS_USN_JOURNAL<br><br>0x02000000         | Yes[&lt;13&gt;](#Appendix_A_13) | File system supports update sequence number (USN) journals.                                                                                                                |
| Reserved<br><br>0xFE007E00                          | N/A                             | These bits fields SHOULD be set to zero when sending and MUST be ignored when the message is received.                                                                     |

#### Unique Identifiers

The SMB Version 1.0 Protocol makes use of the following data types from [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2):

- GUID as specified in section 2.3.4.2

The list of unique identifiers, specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.1.6, has been extended to include the following new unique identifiers:

- 64-bit file identifier (FileId)
- Volume GUID (VolumeGUID)
- [**Copychunk Resume Key**](#gt_7a86a8b5-6ac4-471d-aa3c-9b2bb4638700)

##### FileId Generation

64-bit file identifiers ([**FileIds**](#gt_3b097896-b707-47b5-b1bb-384867a453ea)) are generated on SMB servers. The generation of FileIds MUST satisfy the following constraints:

- The FileId MUST be a 64-bit opaque value.
- The FileId MUST be unique for a file on a given object store.[&lt;14&gt;](#Appendix_A_14)
- The FileId for a file MUST persist for the lifetime of a file on a given object store. A FileId MUST NOT be changed when a file is renamed. When the file is deleted, the FileId MAY be reused.
- All possible values for FileId are valid.

##### VolumeGUID Generation

VolumeGUIDs (Volume Globally Unique Identifiers, or [**volume identifiers**](#gt_892a6724-e635-4ba0-8b8a-d6368f166221), see also [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.3.4) are generated on SMB servers. The generation of VolumeGUIDs MUST satisfy the following constraints:

- The VolumeGUID MUST be a 128-bit opaque value.
- The VolumeGUID MUST be unique for a logical file system volume on a given server.
- The VolumeGUID for the volume can change while the system is running. The VolumeGUID can change when the system is restarted.
- All possible values for the VolumeGUID are valid.

##### Copychunk Resume Key Generation

Copychunk Resume Keys are generated on SMB servers. The generation of Copychunk Resume Keys MUST satisfy the following constraints:

- The Copychunk Resume Key MUST be a 24-byte opaque value generated by an SMB server in response to a request by the client (an SMB_COM_NT_TRANSACTION request with an NT_TRANSACT_IOCTL subcommand for the FSCTL_SRV_REQUEST_RESUME_KEY). For more information, see section [2.2.7.2](#Section_bdcc7363d0c74417b45ae46934b11419).
- The Copychunk Resume Key MUST be unique on the SMB server for a given open file on a server.
- The Copychunk Resume Key MUST remain valid for the lifetime of the open file on the server.
- All possible values for the Copychunk Resume Key are valid.

COPYCHUNK_RESUME_KEY (see sections [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17) and [2.2.7.2.2.2](#Section_c2571af45f264bfcba6738d26f16effc)) represents an opaque data type that contains the server-returned Copychunk Resume Key.

#### Access Masks

The SMB protocol introduces the use of Access Mask structures, which are based on the ACCESS_MASK data type specified in [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.4.3. SMB defines two types of access masks for two basic groups: either for a file, pipe, or printer (specified in section [2.2.1.4.1](#Section_27f99d2977844684b6dd264e9025b286)) or for a directory (specified in section [2.2.1.4.2](#Section_d524144c3cfc49c3903c284e5adbd60a)). Each access mask MUST be a combination of zero or more of the bit positions.

##### File_Pipe_Printer_Access_Mask

The following SMB Access Mask structure is defined for use on a file, named pipe, or printer.

| 0                             | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| File_Pipe_Printer_Access_Mask |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**File_Pipe_Printer_Access_Mask (4 bytes):** For a file, named pipe, or printer, the value MUST be constructed using the following values. For a printer, the value MUST have at least one of the following: FILE_WRITE_DATA, FILE_APPEND_DATA, or GENERIC_WRITE.

| Value                                    | Meaning                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FILE_READ_DATA<br><br>0x00000001         | This value indicates the right to read data from the file or named pipe.                                                                                                                                                                                                                                                                                                                                                      |
| FILE_WRITE_DATA<br><br>0x00000002        | This value indicates the right to write data into the file, named pipe, or printer beyond the end of the file.                                                                                                                                                                                                                                                                                                                |
| FILE_APPEND_DATA<br><br>0x00000004       | This value indicates the right to append data into the file, named pipe, or printer.                                                                                                                                                                                                                                                                                                                                          |
| FILE_READ_EA<br><br>0x00000008           | This value indicates the right to read the extended attributes of the file or named pipe.                                                                                                                                                                                                                                                                                                                                     |
| FILE_WRITE_EA<br><br>0x00000010          | This value indicates the right to write or change the extended attributes to the file or named pipe.                                                                                                                                                                                                                                                                                                                          |
| FILE_EXECUTE<br><br>0x00000020           | This value indicates the right to execute the file.                                                                                                                                                                                                                                                                                                                                                                           |
| FILE_DELETE_CHILD<br><br>0x00000040      | This value indicates the right to delete entries within a directory.                                                                                                                                                                                                                                                                                                                                                          |
| FILE_READ_ATTRIBUTES<br><br>0x00000080   | This value indicates the right to read the attributes of the file.                                                                                                                                                                                                                                                                                                                                                            |
| FILE_WRITE_ATTRIBUTES<br><br>0x00000100  | This value indicates the right to change the attributes of the file.                                                                                                                                                                                                                                                                                                                                                          |
| DELETE<br><br>0x00010000                 | This value indicates the right to delete the file.                                                                                                                                                                                                                                                                                                                                                                            |
| READ_CONTROL<br><br>0x00020000           | This value indicates the right to read the security descriptor for the file or named pipe.                                                                                                                                                                                                                                                                                                                                    |
| WRITE_DAC<br><br>0x00040000              | This value indicates the right to change the [**discretionary access control list (DACL)**](#gt_d727f612-7a45-48e4-9d87-71735d62b321) in the [**security descriptor**](#gt_e5213722-75a9-44e7-b026-8e4833f0d350) for the file or named pipe. For the DACL data structure, see ACL in [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.4.5. |
| WRITE_OWNER<br><br>0x00080000            | This value indicates the right to change the owner in the security descriptor for the file or named pipe.                                                                                                                                                                                                                                                                                                                     |
| SYNCHRONIZE<br><br>0x00100000            | This flag SHOULD NOT be used by the client and MUST be ignored by the server unless on a named pipe as discussed in section [3.2.4.3.1](#Section_28ca564a5aa34ef3a245829627a7b37e) and section [3.3.5.5](#Section_a192be2b06824cc785e2edb544b78d8b).                                                                                                                                                                          |
| ACCESS_SYSTEM_SECURITY<br><br>0x01000000 | This value indicates the right to read or change the [**system access control list (SACL)**](#gt_c189801e-3752-4715-88f4-17804dad5782) in the security descriptor for the file or named pipe. For the SACL data structure, see ACL in \[MS-DTYP\] section 2.4.5.                                                                                                                                                              |
| MAXIMUM_ALLOWED<br><br>0x02000000        | This value indicates that the client is requesting an [**open**](#gt_0d572cce-4683-4b21-945a-7f8035bb6469) to the file with the highest level of access the client has on this file. If no access is granted for the client on this file, the server MUST fail the open with STATUS_ACCESS_DENIED.                                                                                                                            |
| GENERIC_ALL<br><br>0x10000000            | This value indicates a request for all the access flags that are previously listed, except MAXIMUM_ALLOWED and ACCESS_SYSTEM_SECURITY.                                                                                                                                                                                                                                                                                        |
| GENERIC_EXECUTE<br><br>0x20000000        | This value indicates a request for the following combination of access flags listed above: FILE_READ_ATTRIBUTES, FILE_EXECUTE, SYNCHRONIZE, and READ_CONTROL.                                                                                                                                                                                                                                                                 |
| GENERIC_WRITE<br><br>0x40000000          | This value indicates a request for the following combination of access flags listed above: FILE_WRITE_DATA, FILE_APPEND_DATA, FILE_WRITE_ATTRIBUTES, FILE_WRITE_EA, SYNCHRONIZE, and READ_CONTROL.                                                                                                                                                                                                                            |
| GENERIC_READ<br><br>0x80000000           | This value indicates a request for the following combination of access flags listed above: FILE_READ_DATA, FILE_READ_ATTRIBUTES, FILE_READ_EA, SYNCHRONIZE, and READ_CONTROL.                                                                                                                                                                                                                                                 |

##### Directory_Access_Mask

The following SMB Access Mask is defined for use on a directory.

| 0                     | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Directory_Access_Mask |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Directory_Access_Mask (4 bytes):** For a directory, the value MUST be constructed using the following values:

| Value                                    | Meaning                                                                                                                                                                                                                                                                          |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FILE_LIST_DIRECTORY<br><br>0x00000001    | This value indicates the right to enumerate the contents of the directory.                                                                                                                                                                                                       |
| FILE_ADD_FILE<br><br>0x00000002          | This value indicates the right to create a file under the directory.                                                                                                                                                                                                             |
| FILE_ADD_SUBDIRECTORY<br><br>0x00000004  | This value indicates the right to add a sub-directory under the directory.                                                                                                                                                                                                       |
| FILE_READ_EA<br><br>0x00000008           | This value indicates the right to read the extended attributes of the directory.                                                                                                                                                                                                 |
| FILE_WRITE_EA<br><br>0x00000010          | This value indicates the right to write or change the extended attributes of the directory.                                                                                                                                                                                      |
| FILE_TRAVERSE<br><br>0x00000020          | This value indicates the right to traverse this directory if the underlying object store enforces traversal checking.                                                                                                                                                            |
| FILE_DELETE_CHILD<br><br>0x00000040      | This value indicates the right to delete the files and directories within this directory.                                                                                                                                                                                        |
| FILE_READ_ATTRIBUTES<br><br>0x00000080   | This value indicates the right to read the attributes of the directory.                                                                                                                                                                                                          |
| FILE_WRITE_ATTRIBUTES<br><br>0x00000100  | This value indicates the right to change the attributes of the directory.                                                                                                                                                                                                        |
| DELETE<br><br>0x00010000                 | This value indicates the right to delete the directory.                                                                                                                                                                                                                          |
| READ_CONTROL<br><br>0x00020000           | This value indicates the right to read the security descriptor for the directory.                                                                                                                                                                                                |
| WRITE_DAC<br><br>0x00040000              | This value indicates the right to change the DACL in the security descriptor for the directory. For the DACL data structure, see ACL in [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.4.5. |
| WRITE_OWNER<br><br>0x00080000            | This value indicates the right to change the owner in the security descriptor for the directory.                                                                                                                                                                                 |
| SYNCHRONIZE<br><br>0x00100000            | This flag MUST be ignored by both clients and servers.                                                                                                                                                                                                                           |
| ACCESS_SYSTEM_SECURITY<br><br>0x01000000 | This value indicates the right to read or change the SACL in the security descriptor for the directory. For the SACL data structure, see ACL in \[MS-DTYP\] section 2.4.5.                                                                                                       |
| MAXIMUM_ALLOWED<br><br>0x02000000        | This value indicates that the client is requesting an open to the directory with the highest level of access that the client has on this directory. If no access is granted for the client on this directory, then the server MUST fail the open with STATUS_ACCESS_DENIED.      |
| GENERIC_ALL<br><br>0x10000000            | This value indicates a request for all of the access flags that are listed above, except MAXIMUM_ALLOWED and ACCESS_SYSTEM_SECURITY.                                                                                                                                             |
| GENERIC_EXECUTE<br><br>0x20000000        | This value indicates a request for the following access flags listed above: FILE_READ_ATTRIBUTES, FILE_TRAVERSE, SYNCHRONIZE, and READ_CONTROL.                                                                                                                                  |
| GENERIC_WRITE<br><br>0x40000000          | This value indicates a request for the following access flags listed above: FILE_ADD_FILE, FILE_ADD_SUBDIRECTORY, FILE_WRITE_ATTRIBUTES, FILE_WRITE_EA, SYNCHRONIZE, and READ_CONTROL.                                                                                           |
| GENERIC_READ<br><br>0x80000000           | This value indicates a request for the following access flags listed above: FILE_LIST_DIRECTORY, FILE_READ_ATTRIBUTES, FILE_READ_EA, SYNCHRONIZE, and READ_CONTROL.                                                                                                              |

### Defined Constant Extensions

#### SMB_COM Command Codes

No new SMB_COM command codes are introduced other than those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.2.1.[&lt;15&gt;](#Appendix_A_15)

#### Transaction Subcommand Codes

In addition to the transaction subcommand codes specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.2.2, the following modifications and extensions apply. In the following tables, the Description column is also used to specify changes in a particular subcommand's current usage status.

**Transaction Codes used with SMB_COM_TRANSACTION**

| Constant/value                      | Description                                                                                                                                                   |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| TRANS_RAW_READ_NMPIPE<br><br>0x0011 | This command code has changed from [**deprecated**](#gt_129acedf-ccf1-4e13-8df7-3bc59eff6f6d) to [**obsolescent**](#gt_8377951d-81ca-44a2-af83-2a6d6388c121). |
| TRANS_CALL_NMPIPE<br><br>0x0054     | This command code has changed from current to obsolescent.                                                                                                    |

**Transaction Codes used with SMB_COM_TRANSACTION2**

| Constant/value                          | Description                                                                                                                 |
| --------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| TRANS2_SET_FS_INFORMATION<br><br>0x0004 | Set information on a file system on the server. This command code has changed from reserved but not implemented to current. |

**Transaction Codes used with SMB_COM_NT_TRANSACT**

| Constant/value                        | Description                                                                                                                                                                                                                                                                                                                                                                                          |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| NT_TRANSACT_QUERY_QUOTA<br><br>0x0007 | Query a server for a user's disk quota information. This command code is new to these extensions.                                                                                                                                                                                                                                                                                                    |
| NT_TRANSACT_SET_QUOTA<br><br>0x0008   | Set a user's disk quota information on a server. This command code is new to these extensions.                                                                                                                                                                                                                                                                                                       |
| NT_TRANSACT_CREATE2<br><br>0x0009     | This command code is new to these extensions. The client requests and processes the NT_TRANSACT_CREATE2 command the same way it would for an NT_TRANSACT_CREATE command, as specified in \[MS-CIFS\] section 3.2.5.40.1. The server also processes and responds the same way it would for an NT_TRANSACT_CREATE command, as specified in \[MS-CIFS\] section 3.3.5.59.1.[&lt;16&gt;](#Appendix_A_16) |

#### Information Level Codes

The following new Information Level codes are specified in addition to those defined in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.2.3.

##### FIND Information Level Codes

The following new Information Level codes are specified in addition to those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.2.3.1.[&lt;17&gt;](#Appendix_A_17)

| Name                                 | Code   | Meaning                                                              | Dialect   |
| ------------------------------------ | ------ | -------------------------------------------------------------------- | --------- |
| SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO | 0x0105 | Returns the SMB*FIND* FULL_DIRECTORY_INFO data with a FileId.        | NT LANMAN |
| SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO | 0x0106 | Returns the SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO data with a FileId. | NT LANMAN |

##### QUERY_FS Information Level Codes

No new SMB-specific Information Level codes are specified for these extensions.

##### QUERY Information Level Codes

No new SMB-specific Information Level codes are specified for these extensions.

##### SET Information Level Codes

No new SMB-specific Information Level codes are specified for these extensions.

##### Pass-through Information Level Codes

This document provides an extension of a new Information Level code value range called **pass-through Information Levels**, which can be used to set or query information on the server. These Information Levels allow SMB clients to directly query Information Levels native to the underlying object store.[&lt;18&gt;](#Appendix_A_18)

Servers indicate support for these new pass-through Information Levels by setting the new CAP_INFOLEVEL_PASSTHRU capability flag in an SMB_COM_NEGOTIATE server response (section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4)).

To access these new Information Levels, a client adds the constant SMB_INFO_PASSTHROUGH (0x03e8) to the desired native information class level value. This value is then sent in the **InformationLevel** field of the particular SMB_COM_TRANSACTION2 subcommand being used to access the Information Levels.

##### Other Information Level Codes

In addition, SMB_COM_TRANSACTION2 Information Levels in the range 0x200 to 0x3E0 (inclusive) are reserved for third-party extensions, as described in section [1.8](#Section_f99c712ef9984bbc9125cfe2010b6e44).[&lt;19&gt;](#Appendix_A_19)

#### SMB Error Classes and Codes

The following is a list of 32-bit status codes that are required to implement these extensions, their associated values, and a description of what they represent.[&lt;20&gt;](#Appendix_A_20)

| NT status value                                   | Description                                                                                                                                                                                                                                                                                        |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0x00000000<br><br>STATUS_SUCCESS                  | The client request is successful.                                                                                                                                                                                                                                                                  |
| 0x00010002<br><br>STATUS_INVALID_SMB              | An invalid SMB client request is received by the server.                                                                                                                                                                                                                                           |
| 0x00050002<br><br>STATUS_SMB_BAD_TID              | The client request received by the server contains an invalid TID value.                                                                                                                                                                                                                           |
| 0x00160002<br><br>STATUS_SMB_BAD_COMMAND          | The client request received by the server contains an unknown SMB command code.                                                                                                                                                                                                                    |
| 0x005B0002<br><br>STATUS_SMB_BAD_UID              | The client request to the server contains an invalid UID value.                                                                                                                                                                                                                                    |
| 0x00FB0002<br><br>STATUS_SMB_USE_STANDARD         | The client request received by the server is for a non-standard SMB operation (for example, an SMB_COM_READ_MPX request on a non-disk share). The client SHOULD send another request with a different SMB command to perform this operation.                                                       |
| 0x80000005<br><br>STATUS_BUFFER_OVERFLOW          | The data was too large to fit into the specified buffer.                                                                                                                                                                                                                                           |
| 0x80000006<br><br>STATUS_NO_MORE_FILES            | No more files were found that match the file specification.                                                                                                                                                                                                                                        |
| 0x8000002D<br><br>STATUS_STOPPED_ON_SYMLINK       | The create operation stopped after reaching a symbolic link.                                                                                                                                                                                                                                       |
| 0xC0000002<br><br>STATUS_NOT_IMPLEMENTED          | The requested operation is not implemented.                                                                                                                                                                                                                                                        |
| 0xC000000D<br><br>STATUS_INVALID_PARAMETER        | The parameter specified in the request is not valid.                                                                                                                                                                                                                                               |
| 0xC000000E<br><br>STATUS_NO_SUCH_DEVICE           | A device that does not exist was specified.                                                                                                                                                                                                                                                        |
| 0xC0000010<br><br>STATUS_INVALID_DEVICE_REQUEST   | The specified request is not a valid operation for the target device.                                                                                                                                                                                                                              |
| 0xC0000016<br><br>STATUS_MORE_PROCESSING_REQUIRED | If extended security has been negotiated, then this error code can be returned in the SMB_COM_SESSION_SETUP_ANDX response from the server to indicate that additional authentication information is to be exchanged. See section [2.2.4.6](#Section_115b551adcd74ff28c59a334b92e01c0) for details. |
| 0xC0000022<br><br>STATUS_ACCESS_DENIED            | The client did not have the required permission needed for the operation.                                                                                                                                                                                                                          |
| 0xC0000023<br><br>STATUS_BUFFER_TOO_SMALL         | The buffer is too small to contain the entry. No information has been written to the buffer.                                                                                                                                                                                                       |
| 0xC0000034<br><br>STATUS_OBJECT_NAME_NOT_FOUND    | The object name is not found.                                                                                                                                                                                                                                                                      |
| 0xC0000035<br><br>STATUS_OBJECT_NAME_COLLISION    | The object name already exists.                                                                                                                                                                                                                                                                    |
| 0xC000003A<br><br>STATUS_OBJECT_PATH_NOT_FOUND    | The path to the directory specified was not found. This error is also returned on a create request if the operation requires the creation of more than one new directory level for the path specified.                                                                                             |
| 0xC00000A5<br><br>STATUS_BAD_IMPERSONATION_LEVEL  | A specified impersonation level is invalid. This error is also used to indicate that a required impersonation level was not provided.                                                                                                                                                              |
| 0xC00000B5<br><br>STATUS_IO_TIMEOUT               | The specified I/O operation was not completed before the time-out period expired.                                                                                                                                                                                                                  |
| 0xC00000BA<br><br>STATUS_FILE_IS_A_DIRECTORY      | The file that was specified as a target is a directory and the caller specified that it could be anything but a directory.                                                                                                                                                                         |
| 0xC00000BB<br><br>STATUS_NOT_SUPPORTED            | The client request is not supported.                                                                                                                                                                                                                                                               |
| 0xC00000C9<br><br>STATUS_NETWORK_NAME_DELETED     | The network name specified by the client has been deleted on the server. This error is returned if the client specifies an incorrect TID or the share on the server represented by the TID was deleted.                                                                                            |
| 0xC0000203<br><br>STATUS_USER_SESSION_DELETED     | The user session specified by the client has been deleted on the server. This error is returned by the server if the client sends an incorrect UID.                                                                                                                                                |
| 0xC000035C<br><br>STATUS_NETWORK_SESSION_EXPIRED  | The client's session has expired; therefore, the client MUST re-authenticate to continue accessing remote resources.                                                                                                                                                                               |
| 0xC000205A<br><br>STATUS_SMB_TOO_MANY_UIDS        | The client has requested too many UID values from the server or the client already has an [**SMB session**](#gt_ee1ec898-536f-41c4-9d90-b4f7d981fd67) setup with this UID value.                                                                                                                   |

#### Session Key Protection Hash

The SSKeyHash is a well-known constant array.

- BYTE SSKeyHash\[256\] = {
- 0x53, 0x65, 0x63, 0x75, 0x72, 0x69, 0x74, 0x79,
- 0x20, 0x53, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75,
- 0x72, 0x65, 0x20, 0x4b, 0x65, 0x79, 0x20, 0x55,
- 0x70, 0x67, 0x72, 0x61, 0x64, 0x65, 0x79, 0x07,
- 0x6e, 0x28, 0x2e, 0x69, 0x88, 0x10, 0xb3, 0xdb,
- 0x01, 0x55, 0x72, 0xfb, 0x74, 0x14, 0xfb, 0xc4,
- 0xc5, 0xaf, 0x3b, 0x41, 0x65, 0x32, 0x17, 0xba,
- 0xa3, 0x29, 0x08, 0xc1, 0xde, 0x16, 0x61, 0x7e,
- 0x66, 0x98, 0xa4, 0x0b, 0xfe, 0x06, 0x83, 0x53,
- 0x4d, 0x05, 0xdf, 0x6d, 0xa7, 0x51, 0x10, 0x73,
- 0xc5, 0x50, 0xdc, 0x5e, 0xf8, 0x21, 0x46, 0xaa,
- 0x96, 0x14, 0x33, 0xd7, 0x52, 0xeb, 0xaf, 0x1f,
- 0xbf, 0x36, 0x6c, 0xfc, 0xb7, 0x1d, 0x21, 0x19,
- 0x81, 0xd0, 0x6b, 0xfa, 0x77, 0xad, 0xbe, 0x18,
- 0x78, 0xcf, 0x10, 0xbd, 0xd8, 0x78, 0xf7, 0xd3,
- 0xc6, 0xdf, 0x43, 0x32, 0x19, 0xd3, 0x9b, 0xa8,
- 0x4d, 0x9e, 0xaa, 0x41, 0xaf, 0xcb, 0xc6, 0xb9,
- 0x34, 0xe7, 0x48, 0x25, 0xd4, 0x88, 0xc4, 0x51,
- 0x60, 0x38, 0xd9, 0x62, 0xe8, 0x8d, 0x5b, 0x83,
- 0x92, 0x7f, 0xb5, 0x0e, 0x1c, 0x2d, 0x06, 0x91,
- 0xc3, 0x75, 0xb3, 0xcc, 0xf8, 0xf7, 0x92, 0x91,
- 0x0b, 0x3d, 0xa1, 0x10, 0x5b, 0xd5, 0x0f, 0xa8,
- 0x3f, 0x5d, 0x13, 0x83, 0x0a, 0x6b, 0x72, 0x93,
- 0x14, 0x59, 0xd5, 0xab, 0xde, 0x26, 0x15, 0x6d,
- 0x60, 0x67, 0x71, 0x06, 0x6e, 0x3d, 0x0d, 0xa7,
- 0xcb, 0x70, 0xe9, 0x08, 0x5c, 0x99, 0xfa, 0x0a,
- 0x5f, 0x3d, 0x44, 0xa3, 0x8b, 0xc0, 0x8d, 0xda,
- 0xe2, 0x68, 0xd0, 0x0d, 0xcd, 0x7f, 0x3d, 0xf8,
- 0x73, 0x7e, 0x35, 0x7f, 0x07, 0x02, 0x0a, 0xb5,
- 0xe9, 0xb7, 0x87, 0xfb, 0xa1, 0xbf, 0xcb, 0x32,
- 0x31, 0x66, 0x09, 0x48, 0x88, 0xcc, 0x18, 0xa3,
- 0xb2, 0x1f, 0x1f, 0x1b, 0x90, 0x4e, 0xd7, 0xe1
- };

### SMB Message Structure Extensions

#### SMB Header Extensions

All client requests MUST begin with a fixed-size SMB header, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.3.1. All server responses, with the exception of the SMB_COM_READ_RAW response message, as specified in \[MS-CIFS\] section 2.2.4.22.2, MUST begin with the same fixed-size SMB header.

- SMB_Header
- {
- UCHAR Protocol\[4\];
- UCHAR Command;
- SMB_ERROR Status;
- UCHAR Flags;
- USHORT Flags2;
- USHORT PIDHigh;
- UCHAR SecurityFeatures\[8\];
- USHORT Reserved;
- USHORT TID;
- USHORT PIDLow;
- USHORT UID;
- USHORT MID;
- }

The following SMB header fields contain extensions:

| 0        | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8      | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| -------- | --- | --- | --- | --- | --- | --- | --- | ------ | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Protocol |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Command  |     |     |     |     |     |     |     | Status |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...      |     |     |     |     |     |     |     | Flags  |     |            |     |     |     |     |     | Flags2           |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| PIDHigh  |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     | SecurityFeatures |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...      |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...      |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     | Reserved         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| TID      |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     | PIDLow           |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| UID      |     |     |     |     |     |     |     |        |     |            |     |     |     |     |     | MID              |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Flags2 (2 bytes):** The **Flags2** field contains individual bit flags that, depending on the negotiated SMB dialect, indicate various client and server capabilities. This field is defined as specified in \[MS-CIFS\] section 2.2.3.1. There are several new Flags2 values in the SMB header that are not in \[MS-CIFS\], but are part of these extensions. Unused bit fields SHOULD be set to zero by the sender when sending an SMB message and SHOULD[&lt;21&gt;](#Appendix_A_21) be ignored when received by the receiver. This field is constructed using the values listed in section 2.2.3.1 of \[MS-CIFS\], as well as the following additional values:

| Name & bitmask                                           | Meaning                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SMB_FLAGS2_COMPRESSED<br><br>0x0008                      | If set by the client, the client is requesting compressed data for an SMB_COM_READ_ANDX request. If cleared by the server, the server is notifying the client that the data was written uncompressed. This bit field SHOULD only be set to one when NT LAN Manager or later is negotiated for the SMB dialect.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| SMB_FLAGS2_SMB_SECURITY_SIGNATURE_REQUIRED<br><br>0x0010 | This flag SHOULD[&lt;22&gt;](#Appendix_A_22) be set by the client on the first [SMB_COM_SESSION_SETUP_ANDX request (section 2.2.4.6.1)](#Section_a00d03613544484596ab309b4bb7705d) sent to a server that supports extended security if the client requires all further communication with this server to be signed. If the server does not support signing, it MUST disconnect the client by closing the underlying transport connection. Clients and servers MUST ignore this value for other requests and responses. If the client receives a non-signed response from the server, it MUST disconnect the underlying transport connection. This bit field SHOULD only be set to one when NT LAN Manager or later is negotiated for the SMB dialect, the client supports extended security, and the client is configured to require security signatures. |
| SMB_FLAGS2_IS_LONG_NAME<br><br>0x0040                    | If set, the path contained in the message contains long names; otherwise, the paths are restricted to [**8.3 names**](#gt_d2302116-d3d3-4465-a72e-c07a7737b7ae). This bit field SHOULD only be set to one when NT LAN Manager or later is negotiated for the SMB dialect. If client sets this bit in the request, the server SHOULD[&lt;23&gt;](#Appendix_A_23) also set this bit in the response.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| SMB_FLAGS2_REPARSE_PATH<br><br>0x0400                    | If set, the path in the request MUST contain an @GMT token (that is, a Previous Version token), as specified in section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| SMB_FLAGS2_EXTENDED_SECURITY<br><br>0x0800               | Indicates that the client or server supports SPNEGO authentication, as specified in section [3.2.5.2](#Section_d367854f5eee45e8a588eed596a1a521) for client behavior and section [3.3.5.2](#Section_5c005d39b56b4abf83d43847e9ed949c) for server behavior. This bit field SHOULD be set to one only when NT LAN Manager or later is negotiated for the SMB dialect and the client or server supports extended security.                                                                                                                                                                                                                                                                                                                                                                                                                                   |

**PIDHigh (2 bytes):** This field MUST give the 2 high bytes of the [**process identifier (PID)**](#gt_38a420dd-6f31-456e-ae5c-63ec6905380d) if the _Client.Supports32BitPIDs_, as specified in section [3.2.1.1](#Section_585c3763326b4cff8db5a4379509bbaa), is TRUE. Otherwise, it MUST be set to zero.

### SMB Command Extensions

#### SMB_COM_OPEN_ANDX (0x2D)

##### Client Request Extensions

An SMB_COM_OPEN_ANDX request is sent by a client to open a file or named pipe on a server. The new flag value in the **Flags** field of the SMB_COM_OPEN_ANDX request, SMB_OPEN_EXTENDED_RESPONSE, is used to trigger new behavior that is specified in this document. All other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.41.1.

This command has been deprecated. Client implementations SHOULD use SMB_COM_NT_CREATE_ANDX.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT Flags;
- USHORT AccessMode;
- SMB_FILE_ATTRIBUTES SearchAttrs;
- SMB_FILE_ATTRIBUTES FileAttrs;
- UTIME CreationTime;
- USHORT OpenMode;
- ULONG AllocationSize;
- ULONG Timeout;
- USHORT Reserved\[2\];
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- SMB_STRING FileName;
- }
- }

**SMB_Parameters**

Words (34 bytes):

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6              | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | -------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand  |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Flags        |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | AccessMode     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SearchAttrs  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | FileAttrs      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreationTime |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| OpenMode     |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | AllocationSize |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Timeout        |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Reserved       |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |

**Flags (2 bytes):** A 16-bit field of bit flags. For completeness, all flags are listed in the following table. Bit values listed as reserved SHOULD be set to zero by the client and MUST be ignored by the server.

| Name & bitmask                           | Meaning                                                                                                                                                                                                                                                |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| SMB_OPEN_QUERY_INFORMATION<br><br>0x0001 | If set, the client is requesting additional info in the response. The server MUST set **FileDataSize**, **FileAttrs**, **AccessRights**, **ResourceType**, and **NMPipeStatus** in the response. If not set, the server MUST set these fields to zero. |
| SMB_OPEN_OPLOCK<br><br>0x0002            | If set, the client is requesting an [**oplock**](#gt_7b8c743e-84b1-4c7e-83ea-cfb818cdb394).                                                                                                                                                            |
| SMB_OPEN_OPBATCH<br><br>0x0004           | If set, the client is requesting a batch oplock.                                                                                                                                                                                                       |
| SMB_OPEN_EXTENDED_RESPONSE<br><br>0x0010 | If set, the client is requesting the extended format of the response, as described later in this section.                                                                                                                                              |
| Reserved<br><br>0xFFE8                   | Reserved; SHOULD be set to zero by the client, and MUST be ignored by the server.                                                                                                                                                                      |

##### Server Response Extensions

If the client requested extended information by setting SMB_OPEN_EXTENDED_RESPONSE, then a successful response takes the following format. Aside from **WordCount**, **ResourceType**, **ServerFID**, **Reserved**, **MaximalAccessRights**, and **GuestMaximalAccessRights** fields, all other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.41.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT FID;
- SMB_FILE_ATTRIBUTES FileAttrs;
- UTIME LastWriteTime;
- ULONG FileDataSize;
- USHORT AccessRights;
- USHORT ResourceType;
- USHORT NMPipeStatus;
- USHORT OpenResults;
- ULONG ServerFID;
- USHORT Reserved;
- ACCESS_MASK MaximalAccessRights;
- ACCESS_MASK GuestMaximalAccessRights;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- }

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4        | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | -------- | --- | --- | --- | --- | --- | ---------- | --- |
| SMB_Parameters (39 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |          |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |          |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |          |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     | SMB_Data |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |

**SMB_Parameters (39 bytes):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | ---------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| WordCount |     |     |     |     |     |     |     | Words (38 bytes) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |

**WordCount (1 byte):** The value of this field MUST be 0x13.

**Words (38 bytes):**

| 0             | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                        | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ------------------------ | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand   |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset               |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FID           |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | FileAttrs                |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastWriteTime |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                          |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileDataSize  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                          |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AccessRights  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | ResourceType             |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NMPipeStatus  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | OpenResults              |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ServerFID     |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                          |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved      |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | MaximalAccessRights      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...           |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | GuestMaximalAccessRights |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...           |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |

**ResourceType (2 bytes):** The file type. This field MUST be interpreted as follows:

| Name & value                          | Meaning                                                                 |
| ------------------------------------- | ----------------------------------------------------------------------- |
| FileTypeDisk<br><br>0x0000            | File or Directory                                                       |
| FileTypeByteModePipe<br><br>0x0001    | [**Byte mode**](#gt_8586fe9a-1cfa-458a-a145-8db64d69c69c) named pipe    |
| FileTypeMessageModePipe<br><br>0x0002 | [**Message mode**](#gt_c49a48e8-f1ac-4568-bc87-0672eb08868b) named pipe |
| FileTypePrinter<br><br>0x0003         | Printer Device                                                          |
| FileTypeUnknown<br><br>0xFFFF         | Unknown file type                                                       |

**ServerFID (4 bytes):** Reserved but not implemented. Intended as a 32-bit server file identifier that uniquely identifies the file on the server. This field MUST be set to zero by the server and ignored by the client.

**Reserved (2 bytes):** An unused value that SHOULD be set to zero when sending this message. The client MUST ignore this field when receiving this message.

**MaximalAccessRights (4 bytes):** The maximum access rights that this user has on this object. This field MUST be encoded in an ACCESS_MASK format, as specified in section [2.2.1.4](#Section_6e848af95cb64e7383acb68698e3d920).

**GuestMaximalAccessRights (4 bytes):** The maximum access rights that the [**guest account**](#gt_0377d4d6-d45e-4121-8f20-cba8f7fbb7a4) has on this file. This field MUST be encoded in an ACCESS_MASK format, as specified in section 2.2.1.4. Support and exact specifications of the notion of a guest account is implementation specific. Implementations that do not support the notion of a guest account MUST set this field to zero.[&lt;24&gt;](#Appendix_A_24)

**SMB_Data (2 bytes):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ByteCount |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |

**ByteCount (2 bytes):** The value of this field SHOULD[&lt;25&gt;](#Appendix_A_25) be set to zero. The server MUST NOT send any data in this message.

#### SMB_COM_READ_ANDX (0x2E)

##### Client Request Extensions

An SMB_COM_READ_ANDX request is sent by a client to read from a file or named pipe on a server. These extensions overload the **Timeout** field with the new **Timeout_or_MaxCountHigh** field, which allows the use of read lengths above 0xFFFF when CAP_LARGE_READX has been negotiated. All other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.42.1.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT FID;
- ULONG Offset;
- USHORT MaxCountOfBytesToReturn;
- USHORT MinCountOfBytesToReturn;
- ULONG Timeout_or_MaxCountHigh;
- USHORT Remaining;
- ULONG OffsetHigh (optional);
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- }

**SMB_Parameters**

**Words (24 bytes):**

| 0                       | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                       | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ----------------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand             |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset              |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FID                     |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Offset                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | MaxCountOfBytesToReturn |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| MinCountOfBytesToReturn |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Timeout_or_MaxCountHigh |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Remaining               |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| OffsetHigh (optional)   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Timeout_or_MaxCountHigh (4 bytes):** This field is extended to be treated as a union of a 32-bit **Timeout** field and a 16-bit **MaxCountHigh** field. When reading from a regular file, the field MUST be interpreted as **MaxCountHigh** and the two unused bytes MUST be zero. When reading from a name pipe or I/O device, the field MUST be interpreted as **Timeout**.

| 0       | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Timeout |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Timeout (4 bytes):** The client can set the **Timeout** field to a requested time-out value in milliseconds. The client SHOULD[&lt;26&gt;](#Appendix_A_26) set this field to any integer value. The values 0, 0xFFFFFFFF, and 0xFFFFFFFE have special meaning, as specified in \[MS-CIFS\] section 3.3.5.36.

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6        | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | -------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| MaxCountHigh |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Reserved |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**MaxCountHigh (2 bytes):** This field is a 16-bit integer. If the read being requested is larger than 0xFFFF bytes in size, then the client MUST use the **MaxCountHigh** field to hold the two most significant bytes of the requested size, which allows for 32-bit read lengths to be requested when combined with **MaxCountOfBytesToReturn**. If the read is not larger than 0xFFFF bytes, then the client MUST set the **MaxCountHigh** to zero.[&lt;27&gt;](#Appendix_A_27)

**Reserved (2 bytes):** Unlike most other reserved fields, this field can sometimes take specific values. The **Reserved** field SHOULD be set to zero by the client if **MaxCountHigh** is zero, and SHOULD be set to 0xFFFF by the client if **MaxCountHigh** is 0xFFFF. For all other values, this field SHOULD be set to zero by the client. For all values, this field MUST be ignored by the server.

##### Server Response Extensions

A successful response takes the following format. Aside from the first two bytes of the **SMB_Parameters.Words.Reserved2\[\]** field being extended for use as the new **DataLengthHigh** field, all other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.42.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT Available;
- USHORT DataCompactionMode;
- USHORT Reserved1;
- USHORT DataLength;
- USHORT DataOffset;
- USHORT DataLengthHigh;
- USHORT Reserved2\[4\];
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR Pad\[\] (optional);
- UCHAR Data\[variable\];
- }
- }

**SMB_Parameters**

**Words (24 bytes):**

| 0           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                  | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ------------------ | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Available   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | DataCompactionMode |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved1   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | DataLength         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| DataOffset  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | DataLengthHigh     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved2   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                    |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                    |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**DataLengthHigh (2 bytes):** If the data read is greater than or equal to 0x00010000 bytes (64KB) in length, then the server MUST set the two least-significant bytes of the length in the **DataLength** field of the response and the two most-significant bytes of the length in the **DataLengthHigh** field. Otherwise, this field MUST be set to zero.

**Reserved2 (8 bytes):** This field MUST be set to zero by the server and MUST be ignored by the client.

#### SMB_COM_WRITE_ANDX (0x2F)

##### Client Request Extensions

An SMB_COM_WRITE_ANDX request is sent by a client to write data to a file or named pipe on a server. These extensions allocate the **SMB_Parameters.Words.Reserved** field for use as the **DataLengthHigh** field. This field is used when the CAP_LARGE_WRITEX capability has been negotiated to allow for file writes larger than 0xFFFF bytes in length. All other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.43.1.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT FID;
- ULONG Offset;
- ULONG Timeout;
- USHORT WriteMode;
- USHORT Remaining;
- USHORT DataLengthHigh;
- USHORT DataLength;
- USHORT DataOffset;
- ULONG OffsetHigh (optional);
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR Pad;
- UCHAR Data\[variable\];
- }
- }

**SMB_Parameters**

**Words (variable):**

| 0                     | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6              | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | -------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand           |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FID                   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Offset         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Timeout        |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | WriteMode      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Remaining             |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | DataLengthHigh |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| DataLength            |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | DataOffset     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| OffsetHigh (optional) |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**DataLengthHigh (2 bytes):** This field contains the two most significant bytes of the length of the data to write to the file. If the number of bytes to be written is greater than or equal to 0x00010000( 64 kilobytes), then the client MUST set the two least significant bytes of the length in the **DataLength** field of the request and the two most significant bytes of the length in the **DataLengthHigh** field.

##### Server Response Extensions

A successful response takes the following format. These extensions allocate the first two bytes of the **SMB_Parameters.Words.Reserved** field for use as the **CountHigh** field. This field is used when the CAP_LARGE_WRITEX capability has been negotiated to allow for file writes larger than 0xFFFF bytes in length. All other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.43.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT Count;
- USHORT Available;
- USHORT CountHigh;
- USHORT Reserved;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- }

**SMB_Parameters**

**Words (12 bytes):**

| 0           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6          | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Count       |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Available  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CountHigh   |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | Reserved   |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**CountHigh (2 bytes):** This field contains the two most significant bytes of the count of bytes written. If the number of bytes written is greater than or equal to 0x00010000( 64 kilobytes), then the server MUST set the two least significant bytes of the length in the **Count** field of the request and the two most significant bytes of the length in the **CountHigh** field.

**Reserved (2 bytes):** This field is reserved. Servers MUST set this field to zero and clients MUST ignore this field upon receipt.

#### SMB_COM_TRANSACTION2 (0x32) Extensions

The SMB_COM_TRANSACTION2 request is sent by a client to execute a specific operation of various types on the server. These operations include file enumeration, query and set file attribute operations, and DFS referral retrieval. The general format of the SMB_COM_TRANSACTION2 command requests and responses is given in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) sections 2.2.4.46 and 2.2.4.47. Execution of SMB_COM_TRANSACTION2 is defined as specified in \[MS-CIFS\] sections 3.2.4.1.5, 3.2.5.1.4, and 3.3.5.2.5.

Valid SMB_COM_TRANSACTION2 subcommand codes, also known as "Trans2 subcommands", are specified in section [2.2.2.2](#Section_75a3a815d2c64c948d668221869c7975). The format and syntax of these subcommands are specified in section [2.2.6](#Section_a3f5183beedd40e9a13ba4d80eec5d0b) and in \[MS-CIFS\] section 2.2.6.

#### SMB_COM_NEGOTIATE (0x72)

##### Client Request Extensions

All fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.52.1. In order to support the extension in this document, the client MUST include the NT LAN Manager dialect (identified by the "NT LM 0.12" dialect string) in the **SMB_Data.Bytes.Dialects\[\]** array of the request.

When set, the **SMB_Header.Flags2** SMB_FLAGS2_EXTENDED_SECURITY flag indicates support for specification [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378) and GSS authentication (see section [3.1.5.1](#Section_2fa60c5a71ee4248a4e9dd5d0db2373d)), and indicates to the server that it sends an Extended Security response (see section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4)).

##### Server Response Extensions

###### Extended Security Response

If the selected dialect is NT LAN Manager and the client has indicated extended security is being used, a successful response MUST take the following form. Aside from the additional notes to the **SMB_Parameters.Words.MaxBufferSize** and **SMB_Parameters.Words.ChallengeLength** fields, the new **SMB_Parameters.Words.Capabilities** bits, and the **SMB_Data.Bytes.ServerGuid** and **SMB_Data.Bytes.SecurityBlob** fields, all other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.52.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- USHORT DialectIndex;
- UCHAR SecurityMode;
- USHORT MaxMpxCount;
- USHORT MaxNumberVcs;
- ULONG MaxBufferSize;
- ULONG MaxRawSize;
- ULONG SessionKey;
- ULONG Capabilities;
- FILETIME SystemTime;
- SHORT ServerTimeZone;
- UCHAR ChallengeLength;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- GUID ServerGUID;
- UCHAR SecurityBlob\[\];
- }
- }

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4                   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | ------------------- | --- | --- | --- | --- | --- | ---------- | --- |
| SMB_Parameters (35 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     | SMB_Data (variable) |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |

**SMB_Parameters (35 bytes):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | ---------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| WordCount |     |     |     |     |     |     |     | Words (34 bytes) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |

**Words (34 bytes):**

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8               | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6            | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4              | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | --------------- | --- | ---------- | --- | --- | --- | --- | --- | ------------ | --- | --- | --- | ---------- | --- | --- | --- | -------------- | --- | --- | --- | --- | --- | ---------- | --- |
| DialectIndex |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     | SecurityMode |     |     |     |            |     |     |     | MaxMpxCount    |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     | MaxNumberVcs    |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | MaxBufferSize  |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | MaxRawSize     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | SessionKey     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | Capabilities   |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | SystemTime     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     |                |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | ServerTimeZone |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     | ChallengeLength |     |            |     |     |     |     |     |

**MaxBufferSize (4 bytes):** Maximum size, in bytes, of the server buffer for receiving SMB messages. This value accounts for the size of the largest SMB message that the client can send to the server, measured from the start of the SMB header to the end of the packet. This value does not account for any underlying transport-layer packet headers, and thus does not account for the size of the complete network packet.[&lt;28&gt;](#Appendix_A_28)

The only cases in which this maximum buffer size MUST be exceeded are:

- When the SMB_COM_WRITE_ANDX command is used and the client and server both support the CAP_LARGE_WRITEX capability (see the **Capabilities** field for more information).
- When the SMB_COM_WRITE_RAW command is used and both the client and server support the CAP_RAW_MODE capability.

**Capabilities (4 bytes):** A 32-bit field providing a set of server capability indicators. This bit field is used to indicate to the client which features are supported by the server. Any value not listed in the following table is unused. The server MUST set the unused bits to zero. The client MUST ignore these bits.

These extensions provide the following new capability bits:

- CAP_COMPRESSED_DATA
- CAP_DYNAMIC_REAUTH
- CAP_EXTENDED_SECURITY
- CAP_INFOLEVEL_PASSTHRU
- CAP_LARGE_WRITEX
- CAP_LWIO
- CAP_UNIX

The rest of the values in the capabilities table are included for completeness.

| Name and bitmask                         | Meaning                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CAP_RAW_MODE<br><br>0x00000001           | The server supports SMB_COM_READ_RAW and SMB_COM_WRITE_RAW requests.[&lt;29&gt;](#Appendix_A_29) Raw mode is not supported over connectionless transports.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| CAP_MPX_MODE<br><br>0x00000002           | The server supports SMB_COM_READ_MPX and SMB_COM_WRITE_MPX requests.[&lt;30&gt;](#Appendix_A_30) MPX mode is supported only over connectionless transports.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| CAP_UNICODE<br><br>0x00000004            | The server supports UTF-16LE [**Unicode strings**](#gt_b069acb4-e364-453e-ac83-42d469bb339e).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| CAP_LARGE_FILES<br><br>0x00000008        | The server supports large files with 64-bit offsets.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| CAP_NT_SMBS<br><br>0x00000010            | The server supports SMB commands particular to the NT LAN Manager dialect.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| CAP_RPC_REMOTE_APIS<br><br>0x00000020    | The server supports the use of remote procedure call [\[MS-RPCE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-RPCE%5d.pdf#Section_290c38b192fe422991e64fc376610c15) for remote API calls. Similar functionality would otherwise require use of the legacy Remote Administration Protocol, as specified in [\[MS-RAP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-RAP%5d.pdf#Section_fb8d5bd1e57c4be1b063ec31330bdd58).                                                                                                                                                                                                                                                                                                         |
| CAP_STATUS32<br><br>0x00000040           | The server is capable of responding with 32-bit status codes in the **Status** field of the SMB header (for more information, see \[MS-CIFS\] 2.2.3.1). CAP_STATUS32 can also be referred to as CAP_NT_STATUS.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| CAP_LEVEL_II_OPLOCKS<br><br>0x00000080   | The server supports level II opportunistic locks (oplocks).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| CAP_LOCK_AND_READ<br><br>0x00000100      | The server supports the SMB_COM_LOCK_AND_READ command requests.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| CAP_NT_FIND<br><br>0x00000200            | The server supports the TRANS2_FIND_FIRST2, TRANS2_FIND_NEXT2, and FIND_CLOSE2 command requests. This bit SHOULD[&lt;31&gt;](#Appendix_A_31) be set if CAP_NT_SMBS is set.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| CAP_DFS<br><br>0x00001000                | The server is aware of the DFS Referral Protocol, as specified in [\[MS-DFSC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSC%5d.pdf#Section_3109f4be2dbb42c99b8e0b34f7a2135e), and can respond to DFS referral requests. For more information, see \[MS-CIFS\] sections 2.2.6.16.1 and 2.2.6.16.2.                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| CAP_INFOLEVEL_PASSTHRU<br><br>0x00002000 | The server supports pass-through Information Levels, as specified in section [2.2.2.3](#Section_51380f0586164df6998c26d65d7d6ca8). This allows the client to pass Information Level structures in QUERY and SET operations.[&lt;32&gt;](#Appendix_A_32)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| CAP_LARGE_READX<br><br>0x00004000        | The server supports large read operations. This capability affects the maximum size, in bytes, of the server buffer for sending an SMB_COM_READ_ANDX response to the client. When this capability is set by the server (and set by the client in the SMB_COM_SESSION_SETUP_ANDX request), then the maximum server buffer size for sending data can exceed the **MaxBufferSize** field. Therefore, the server can send a single SMB_COM_READ_ANDX response to the client up to an implementation-specific default size.[&lt;33&gt;](#Appendix_A_33)<br><br>When signing is active on a connection, then clients MUST limit read lengths to the **MaxBufferSize** value negotiated by the server irrespective of the value of the CAP_LARGE_READX flag. |
| CAP_LARGE_WRITEX<br><br>0x00008000       | The server supports large write operations. This capability affects the maximum size, in bytes, of the server buffer for receiving an SMB_COM_WRITE_ANDX client request. When this capability is set by the server (and set by the client in the SMB_COM_SESSION_SETUP_ANDX request), then the maximum server buffer size of bytes it writes can exceed the **MaxBufferSize** field. Therefore, a client can send a single SMB_COM_WRITE_ANDX request up to this size.[&lt;34&gt;](#Appendix_A_34)<br><br>When signing is active on a connection, then clients MUST limit write lengths to the **MaxBufferSize** value negotiated by the server, irrespective of the value of the CAP_LARGE_WRITEX flag.                                              |
| CAP_LWIO<br><br>0x00010000               | The server supports new light-weight I/O control ([**IOCTL**](#gt_09d6bc87-34ed-48e8-b4d4-962e90543462)) and file system control (FSCTL) operations. These operations are accessed using the NT_TRANSACT_IOCTL subcommand (section [2.2.7.2](#Section_bdcc7363d0c74417b45ae46934b11419)).[&lt;35&gt;](#Appendix_A_35)                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| CAP_UNIX<br><br>0x00800000               | The server supports UNIX extensions.[&lt;36&gt;](#Appendix_A_36) For more information, see [\[SNIA\]](http://go.microsoft.com/fwlink/?LinkId=90519).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| CAP_COMPRESSED_DATA<br><br>0x02000000    | Reserved but not implemented.[&lt;37&gt;](#Appendix_A_37)<br><br>The server supports compressed SMB packets.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| CAP_DYNAMIC_REAUTH<br><br>0x20000000     | The server supports re-authentication.[&lt;38&gt;](#Appendix_A_38)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| CAP_PERSISTENT_HANDLES<br><br>0x40000000 | Reserved but not implemented.[&lt;39&gt;](#Appendix_A_39)<br><br>The server supports persistent handles.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| CAP_EXTENDED_SECURITY<br><br>0x80000000  | The server supports extended security for authentication, as specified in section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953). This bit is used in conjunction with the SMB_FLAGS2_EXTENDED_SECURITY **SMB_Header.Flags2** flag, as specified in section [2.2.3.1](#Section_3c0848a6efe947c2b57af7e8217150b9).                                                                                                                                                                                                                                                                                                                                                                                                                             |

**ChallengeLength (1 byte):** When the CAP_EXTENDED_SECURITY bit is set, the server MUST set this value to zero and clients MUST ignore this value.

**SMB_Data (variable):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ByteCount |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Bytes (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ByteCount (2 bytes):** The number of bytes in the **SMB_Data.Bytes** array, which follows. This field MUST be greater than or equal to 0x0010.

**Bytes (variable):**

| 0                       | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ServerGUID (16 bytes)   |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SecurityBlob (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ServerGUID (16 bytes):** This field MUST be a GUID generated by the server to uniquely identify this server. This field SHOULD NOT be used by a client as a secure method of identifying a server because it can be forged. A client SHOULD use this information to detect whether connections to different textual names resolve to the same target server when direct TCP is used. This knowledge can then be used to set the **SMB_Parameters.Words.VcNumber** field in the SMB_COM_SESSION_SETUP_ANDX request (see \[MS-CIFS\] section 2.2.4.53.1).[&lt;40&gt;](#Appendix_A_40)

**SecurityBlob (variable):** A security binary large object (BLOB) that SHOULD contain an authentication token as produced by the GSS protocol (as specified in section 3.2.4.2.4 and [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378)).

###### Non-Extended Security Response

If extended security is not being used and the NT LAN Manager dialect has been selected, then a successful response MUST take the following form. Aside from the new **SMB_Parameters.Words.Capabilities** bits, the additional notes to the **SMB_Parameters.Words.MaxBufferSize** field, and the **SMB_Data.Bytes.ServerName** field, all other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.52.2. The **SMB_Parameters.Words.ChallengeLength** field and the entire **SMB_Data** block are included from \[MS-CIFS\] to highlight the differences between the Extended and Non-Extended Security responses.

In order to determine whether the **SMB_Data.Bytes.ServerName** field is present, the client MUST check the **SMB_Data.ByteCount** field to determine whether additional data is present beyond the NULL terminator of the **SMB_Data.Bytes.DomainName** string.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- USHORT DialectIndex;
- UCHAR SecurityMode;
- USHORT MaxMpxCount;
- USHORT MaxNumberVcs;
- ULONG MaxBufferSize;
- ULONG MaxRawSize;
- ULONG SessionKey;
- ULONG Capabilities;
- FILETIME SystemTime;
- SHORT ServerTimeZone;
- UCHAR ChallengeLength;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR Challenge\[\];
- SMB_STRING DomainName\[\];
- SMB_STRING ServerName\[\];
- }
- }

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4                   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | ------------------- | --- | --- | --- | --- | --- | ---------- | --- |
| SMB_Parameters (35 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     | SMB_Data (variable) |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |                     |     |     |     |     |     |            |     |

**SMB_Parameters (35 bytes):**

| 0                | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ---------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Words (34 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...              |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...              |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...              |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |

**Words (34 bytes):**

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8               | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6            | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4              | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | --------------- | --- | ---------- | --- | --- | --- | --- | --- | ------------ | --- | --- | --- | ---------- | --- | --- | --- | -------------- | --- | --- | --- | --- | --- | ---------- | --- |
| DialectIndex |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     | SecurityMode |     |     |     |            |     |     |     | MaxMpxCount    |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     | MaxNumberVcs    |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | MaxBufferSize  |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | MaxRawSize     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | SessionKey     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | Capabilities   |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | SystemTime     |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     |                |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     |                 |     |            |     |     |     |     |     |              |     |     |     |            |     |     |     | ServerTimeZone |     |     |     |     |     |            |     |
| ...          |     |     |     |     |     |     |     | ChallengeLength |     |            |     |     |     |     |     |

**MaxBufferSize (4 bytes):** Maximum size, in bytes, of the server buffer for receiving SMB messages. This value indicates the size of the largest SMB message that the server is capable of receiving from the client, measured from the start of the SMB header to the end of the packet. This value does not account for any underlying transport-layer packet headers and thus does not account for the size of the complete network packet.[&lt;41&gt;](#Appendix_A_41)

The only exceptions in which this maximum buffer size can be exceeded are:

- When the SMB_COM_WRITE_ANDX command is used and both the client and server support the CAP_LARGE_WRITEX capability (see the **Capabilities** field for more information).
- When the SMB_COM_READ_ANDX command is used and both the client and server support the CAP_LARGE_READX capability (see the Capabilities field for more information).
- When the SMB_COM_WRITE_RAW command is used and both the client and server support the CAP_RAW_MODE capability.

**Capabilities (4 bytes):** A 32-bit field providing a set of server capability indicators. This bit field is used to indicate to the client which features are supported by the server. Any value not listed in the following table is unused. The server MUST set the unused bits to zero in a response and the client MUST ignore these bits.

There are several new capability bits:

- CAP_COMPRESSED_DATA
- CAP_DYNAMIC_REAUTH
- CAP_EXTENDED_SECURITY
- CAP_INFOLEVEL_PASSTHRU
- CAP_LARGE_WRITEX
- CAP_LWIO
- CAP_UNIX

Any value not listed in the following table SHOULD be unused. A server SHOULD set the unused bits to zero in a response and a client MUST ignore these bits. The table of server capabilities is provided in the previous section.

**ChallengeLength (1 byte):** The value of this field MUST be 0x08 and is the length of the random challenge used in challenge/response authentication. This field is often referred to as EncryptionKeyLength.

**SMB_Data (variable):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ByteCount |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Bytes (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ByteCount (2 bytes):** This field MUST be greater than or equal to 0x0003.

**Bytes (variable):**

| 0                     | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Challenge (variable)  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                   |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| DomainName (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                   |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ServerName (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                   |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Challenge (variable):** An array of unsigned bytes that MUST be the length of the number of bytes specified in the **ChallengeLength** field and MUST represent the server challenge. This array MUST NOT be NULL-terminated.[&lt;42&gt;](#Appendix_A_42)

**DomainName (variable):** The name of the [**domain**](#gt_b0276eb2-4e65-4cf1-a718-e0920a614aca) or workgroup to which the server belongs.

**ServerName (variable):** A variable-length, NULL-terminated Unicode string that contains the name of the Server.

#### SMB_COM_SESSION_SETUP_ANDX (0x73)

##### Client Request Extensions

An SMB_COM_SESSION_SETUP_ANDX request MUST be sent by a client to begin user authentication on an SMB connection and establish an SMB session.

When extended security is being used (see section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953)), the request MUST take the following form. Aside from the **SecurityBlobLength** field, the additional capabilities used in the **Capabilities** field, and the **ByteCount** and **SecurityBlob** fields, all other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.53.1.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT MaxBufferSize;
- USHORT MaxMpxCount;
- USHORT VcNumber;
- ULONG SessionKey;
- USHORT SecurityBlobLength;
- ULONG Reserved;
- ULONG Capabilities;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR SecurityBlob\[SecurityBlobLength\];
- SMB_STRING NativeOS\[\];
- SMB_STRING NativeLanMan\[\];
- }
- }

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | ------------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SMB_Parameters (25 bytes) |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     | SMB_Data (variable) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SMB_Parameters (25 bytes):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | ---------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| WordCount |     |     |     |     |     |     |     | Words (24 bytes) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |

**WordCount (1 byte):** The value of this field MUST be 0x0C.

**Words (24 bytes):**

| 0             | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                  | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ------------------ | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand   |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| MaxBufferSize |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | MaxMpxCount        |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| VcNumber      |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | SessionKey         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...           |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | SecurityBlobLength |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved      |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                    |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Capabilities  |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |                    |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SecurityBlobLength (2 bytes):** This value MUST specify the length in bytes of the variable-length **SecurityBlob** field that is contained within the request.

**Capabilities (4 bytes):** A set of client capabilities. This field has the same structure as the **SMB_Parameters.Capabilities** field of the SMB_COM_NEGOTIATE Server Response specified in section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4).[&lt;43&gt;](#Appendix_A_43)

**SMB_Data (variable):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ByteCount |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Bytes (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ByteCount (2 bytes):** If SMB_FLAGS2_UNICODE is set in the **SMB_Header.Flags2** field, then this field MUST be greater than or equal to 0x0004. If SMB_FLAGS2_UNICODE is not set, then this field MUST be greater than or equal to 0x0002.

**Bytes (variable):**

| 0                       | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SecurityBlob (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NativeOS (variable)     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NativeLanMan (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SecurityBlob (variable):** This field MUST be the authentication token sent to the server, as specified in section 3.2.4.2.4 and in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378).

**NativeOS (variable):** A string that represents the native operating system of the SMB client. If SMB_FLAGS2_UNICODE is set in the **Flags2** field of the SMB header of the request, then the name string MUST be a NULL-terminated array of 16-bit Unicode characters. Otherwise, the name string MUST be a NULL-terminated array of [**OEM characters**](#gt_681188c8-235a-47f5-af29-7fbd0676a6b8). If the name string consists of Unicode characters, then this field MUST be aligned to start on a 2-byte boundary from the start of the SMB header.[&lt;44&gt;](#Appendix_A_44)

**NativeLanMan (variable):** A string that represents the native LAN manager type of the client. If SMB_FLAGS2_UNICODE is set in the **Flags2** field of the SMB header of the request, then the name string MUST be a NULL-terminated array of 16-bit Unicode characters. Otherwise, the name string MUST be a NULL-terminated array of OEM characters. If the name string consists of Unicode characters, then this field MUST be aligned to start on a 2-byte boundary from the start of the SMB header.[&lt;45&gt;](#Appendix_A_45)

##### Server Response Extensions

When extended security is being used (see section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953)), a successful response MUST take the following form. Aside from the SecurityBlobLength field, the additional capabilities used in the Capabilities field, the ByteCount and SecurityBlob fields, and the omission of the PrimaryDomain field, all of the other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.53.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT Action;
- USHORT SecurityBlobLength;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR SecurityBlob\[SecurityBlobLength\];
- SMB_STRING NativeOS\[\];
- SMB_STRING NativeLanMan\[\];

- }
- }

| 0              | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| -------------- | --- | --- | --- | --- | --- | --- | --- | ------------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SMB_Parameters |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...            |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...            |     |     |     |     |     |     |     | SMB_Data (variable) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...            |     |     |     |     |     |     |     |                     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SMB_Parameters (9 bytes):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8     | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | ----- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| WordCount |     |     |     |     |     |     |     | Words |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |       |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |

**WordCount (1 byte):** The value of this field MUST be 0x04.

**Words (8 bytes):**

| 0           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                  | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ------------------ | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset         |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Action      |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | SecurityBlobLength |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Action (2 bytes):** A 16-bit field. The two lowest-order bits have been defined.

| Name and bitmask                       | Meaning                                                                                                                                                                                           |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SMB_SETUP_GUEST<br><br>0x0001          | If clear (0), then the user successfully authenticated and is logged in.<br><br>If set (1), then authentication failed but the server has granted guest access; the user is logged in as a Guest. |
| SMB_SETUP_USE_LANMAN_KEY<br><br>0x0002 | This bit is not used with extended security and MUST be clear.                                                                                                                                    |

The server's response does not specify whether the access granted is of type Anonymous. However, the security system can provide that information once authorization completes.

**SecurityBlobLength (2 bytes):** This value MUST specify the length, in bytes, of the variable-length **SecurityBlob** that is contained within the response.

**SMB_Data (variable):**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ByteCount |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Bytes (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ByteCount (2 bytes):** If SMB_FLAGS2_UNICODE is set in the **SMB_Header.Flags2** field, then this field MUST be greater than or equal to 0x0006. If SMB_FLAGS2_UNICODE is not set, then this field MUST be greater than or equal to 0x0003.

**Bytes (variable):**

| 0                       | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SecurityBlob (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NativeOS (variable)     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NativeLanMan (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SecurityBlob (variable):** This value MUST contain the authentication token being returned to the client, as specified in section [3.3.5.3](#Section_1f152df0a61d4e769af6da96fa783c02) and [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378).

**NativeOS (variable):** A string that represents the native operating system of the server. If SMB_FLAGS2_UNICODE is set in the **Flags2** field of the SMB header of the response, then the string MUST be a NULL-terminated array of 16-bit Unicode characters. Otherwise, the string MUST be a NULL-terminated array of OEM characters. If the name string consists of Unicode characters, then this field MUST be aligned to start on a 2-byte boundary from the start of the SMB header.

**NativeLanMan (variable):** A string that represents the native LAN Manager type of the server. If SMB_FLAGS2_UNICODE is set in the **Flags2** field of the SMB header of the response, then the string MUST be a NULL-terminated array of 16-bit Unicode characters. Otherwise, the string MUST be a NULL-terminated array of OEM characters. If the name string consists of Unicode characters, then this field MUST be aligned to start on a 2-byte boundary from the start of the SMB header.

#### SMB_COM_TREE_CONNECT_ANDX (0x75)

##### Client Request Extensions

An SMB_COM_TREE_CONNECT_ANDX request MUST be sent by a client to establish a [**tree connect**](#gt_c65d1989-3473-4fa9-ac45-6522573823e3) to a share. These extensions define new flags (TREE_CONNECT_ANDX_EXTENDED_SIGNATURES and TREE_CONNECT_ANDX_EXTENDED_RESPONSE) in the **SMB_Parameters.Words.Flags** field that are used to trigger the new behavior defined in this specification. The full field description from [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) is included for completeness. All other fields are as specified in \[MS-CIFS\] section 2.2.4.55.1.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT Flags;
- USHORT PasswordLength;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- UCHAR Password\[PasswordLength\];
- UCHAR Pad\[\];
- SMB_STRING Path;
- OEM_STRING Service;
- }
- }

**SMB_Parameters**

**Words (8 bytes):**

| 0           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6              | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | -------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Flags       |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | PasswordLength |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Flags (2 bytes):** A set of options that modify the SMB_COM_TREE_CONNECT_ANDX request. The entire flag set is given here with its symbolic constants. Any combination of the following flags is valid. Any values not given as follows are considered reserved.

| Name & bitmask                                      | Meaning                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| TREE_CONNECT_ANDX_DISCONNECT_TID<br><br>0x0001      | If set and **SMB_Header.TID** is valid, the tree connect specified by the TID in the SMB header of the request SHOULD be disconnected when the server sends the response. If this tree disconnect fails, then the error SHOULD be ignored.<br><br>If set and TID is invalid, the server MUST ignore this bit. |
| TREE_CONNECT_ANDX_EXTENDED_SIGNATURES<br><br>0x0004 | If set, then the client is requesting signing key protection, as specified in sections [3.2.4.2.5](#Section_d4f09ef1a9aa4530a2ae438fd601e2e3) and [3.2.5.4](#Section_66aeb6701e484cb9a6517c70f355cd16).                                                                                                       |
| TREE_CONNECT_ANDX_EXTENDED_RESPONSE<br><br>0x0008   | If set, then the client is requesting extended information in the SMB_COM_TREE_CONNECT_ANDX response.                                                                                                                                                                                                         |

##### Server Response Extensions

When a server returns extended information, the response takes the following format. Aside from the **WordCount**, **MaximalShareAccessRights**, and **GuestMaximalShareAccessRights** fields, and the new **OptionalSupport** flags, all other fields are defined as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.55.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- USHORT OptionalSupport;
- ACCESS_MASK MaximalShareAccessRights;
- ACCESS_MASK GuestMaximalShareAccessRights;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- OEM_STRING Service;
- UCHAR Pad\[\];
- SMB_STRING NativeFileSystem;
- }
- }

**SMB_Parameters**

| 0         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------- | --- | --- | --- | --- | --- | --- | --- | ---------------- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| WordCount |     |     |     |     |     |     |     | Words (14 bytes) |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...       |     |     |     |     |     |     |     |                  |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |

**WordCount (1 byte):** The value of this field MUST be 0x07.

**Words (14 bytes):**

| 0               | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                             | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ----------------------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand     |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset                    |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| OptionalSupport |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | MaximalShareAccessRights      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...             |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     | GuestMaximalShareAccessRights |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...             |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |

**OptionalSupport (2 bytes):** The following **OptionalSupport** bit fields are new extensions: SMB_CSC_MASK, SMB_UNIQUE_FILE_NAME, and SMB_EXTENDED_SIGNATURES. Values from \[MS-CIFS\] are included for completeness. The values of SMB_CSC_MASK each have their own name and are included for reference purposes only. Any combination of the following flags MUST be supported. All undefined values are considered reserved. The server SHOULD set them to zero and the client MUST ignore them.

| Name & bitmask                        | Value                                                                                                                                                                                               | Meaning                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SMB_SUPPORT_SEARCH_BITS<br><br>0x0001 | 0                                                                                                                                                                                                   | If set, then the server supports the use of SMB_FILE_ATTRIBUTES Search Attributes in client directory search requests, as specified in \[MS-CIFS\].                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 1                                     |
| SMB_SHARE_IS_IN_DFS<br><br>0x0002     | 0                                                                                                                                                                                                   | If set, this share is managed by Distributed File System (DFS), as specified in [\[MS-DFSC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSC%5d.pdf#Section_3109f4be2dbb42c99b8e0b34f7a2135e).                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 1                                     |
| SMB_CSC_MASK<br><br>0x000C            | 0                                                                                                                                                                                                   | SMB_CSC_CACHE_MANUAL_REINT Clients are allowed to cache files that the user requests for offline use, but there is no automatic file-by-file reintegration.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 1                                     | SMB_CSC_CACHE_AUTO_REINT Clients are allowed to automatically cache the files that a user or application modifies for offline use. Automatic file-by-file reintegration MUST be permitted.          |
| 2                                     | SMB_CSC_CACHE_VDO Clients are allowed to automatically cache the files that a user or application modifies for offline use. Clients are permitted to work from their local cache even while online. |
| 3                                     | SMB_CSC_NO_CACHING No offline caching is allowed for this share.                                                                                                                                    |
| SMB_UNIQUE_FILE_NAME<br><br>0x0010    | 0                                                                                                                                                                                                   | If set, then the server is using long file names only and does not support short file names. If set, then the server allows the client to assume that there is no name aliasing for this share (in other words, a single file cannot have two different names). If set, then the server permits the client to cache directory enumerations and file metadata based on the pathname.<br><br>The client MAY[&lt;46&gt;](#Appendix_A_46) choose to satisfy file attribute queries from its cache and thus could present a slightly stale view of files on the share. The client MUST NOT cache remote file system information for more than 60 seconds. |
| 1                                     |
| SMB_EXTENDED_SIGNATURES<br><br>0x0020 | 0                                                                                                                                                                                                   | If set, then the server is using signing key protection (see section [3.3.5.4](#Section_8e1132db35514a439552326236eb2c67)), as requested by the client.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| 1                                     |

**MaximalShareAccessRights (4 bytes):** This field MUST specify the maximum rights that the user has to this share based on the security enforced by the share. This field MUST be encoded in an ACCESS_MASK format, as specified in section [2.2.1.4](#Section_6e848af95cb64e7383acb68698e3d920).

**GuestMaximalShareAccessRights (4 bytes):** This field MUST specify the maximum rights that the guest account has on this share based on the security enforced by the share. Note that the notion of a guest account is implementation specific.[&lt;47&gt;](#Appendix_A_47)

Implementations that do not support the notion of a guest account MUST set this field to zero, which implies no access. This field MUST be encoded in an ACCESS_MASK format, as specified in section 2.2.1.4.

#### SMB_COM_NT_TRANSACT (0xA0) Extensions

The SMB_COM_NT_TRANSACT request is sent by a client to specify operations on the server. The operations include file open, file create, device I/O control, notify directory change, and set and query security descriptors. The general format of the SMB_COM_NT_TRANSACT command requests and responses is given in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) sections 2.2.4.62 and 2.2.4.63. Execution of SMB_COM_NT_TRANSACT is as specified in \[MS-CIFS\] sections 3.2.4.1.5, 3.2.5.1.4, and 3.3.5.2.5.

Valid SMB_COM_NT_TRANSACT subcommand codes, also known as "NT Trans subcommand" codes, are specified in section [2.2.2.2](#Section_75a3a815d2c64c948d668221869c7975). The format and syntax of these subcommands are specified in section [2.2.7](#Section_64c604b9394b4ad7b7fd167271882359) and in \[MS-CIFS\] section 2.2.7.

#### SMB_COM_NT_CREATE_ANDX (0xA2)

##### Client Request Extensions

An SMB_COM_NT_CREATE_ANDX request is sent by a client to open a file or device on the server. This extension adds the following:

- An additional flag bit is added to the **Flags** field of the SMB_COM_NT_CREATE_ANDX request. The additional flag, NT_CREATE_REQUEST_EXTENDED_RESPONSE, is used to request an extended response from the server.
- An additional parameter value is added to the **ImpersonationLevel** field. SECURITY_DELEGATION is added to allow the server to call other servers while impersonating the original client.

All other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.64.1.[&lt;48&gt;](#Appendix_A_48)

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- UCHAR Reserved;
- USHORT NameLength;
- ULONG Flags;
- ULONG RootDirectoryFID;
- ULONG DesiredAccess;
- LARGE_INTEGER AllocationSize;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- ULONG ShareAccess;
- ULONG CreateDisposition;
- ULONG CreateOptions;
- ULONG ImpersonationLevel;
- UCHAR SecurityFlags;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- Bytes
- {
- SMB_STRING FileName;
- }
- }

**SMB_Parameters**

**Words (48 bytes):**

| 0           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8            | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6          | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4                  | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------- | --- | --- | --- | --- | --- | --- | --- | ------------ | --- | ---------- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | ---------- | --- | --- | --- | ------------------ | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand |     |     |     |     |     |     |     | AndXReserved |     |            |     |     |     |     |     | AndXOffset |     |     |     |            |     |     |     |                    |     |     |     |     |     |            |     |
| Reserved    |     |     |     |     |     |     |     | NameLength   |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | Flags              |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | RootDirectoryFID   |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | DesiredAccess      |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | AllocationSize     |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                    |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | ExtFileAttributes  |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | ShareAccess        |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | CreateDisposition  |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | CreateOptions      |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | ImpersonationLevel |     |     |     |     |     |            |     |
| ...         |     |     |     |     |     |     |     |              |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | SecurityFlags      |     |     |     |     |     |            |     |

**Flags (4 bytes):** A set of flags that modify the client request, as defined in the table below. NT_CREATE_REQUEST_EXTENDED_RESPONSE is new to MS-SMB. All other flags are included in the table for completeness. Unused bits SHOULD be set to zero by the client when sending a request and MUST be ignored when received by the server.

| Name & bitmask                                        | Meaning                                                                                    |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| NT_CREATE_REQUEST_OPLOCK<br><br>0x00000002            | If set, then the client is requesting an oplock.                                           |
| NT_CREATE_REQUEST_OPBATCH<br><br>0x00000004           | If set, then the client is requesting a batch oplock.                                      |
| NT_CREATE_OPEN_TARGET_DIR<br><br>0x00000008           | If set, then the client indicates that the parent directory of the target is to be opened. |
| NT_CREATE_REQUEST_EXTENDED_RESPONSE<br><br>0x00000010 | If set, then the client is requesting extended information in the response.                |

**ImpersonationLevel (4 bytes):** This field specifies the impersonation level requested by the application that is issuing the create request, and MUST contain one of the following values. The server MUST validate this field, but otherwise ignore it.

Impersonation is described in [\[MS-WPO\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-WPO%5d.pdf#Section_c5f54a7765be40a0bb829e4181d8ab67) section 9.7; for more information about impersonation, see [\[MSDN-IMPERS\]](http://go.microsoft.com/fwlink/?LinkId=106009).

| Value                                     | Meaning                                                          |
| ----------------------------------------- | ---------------------------------------------------------------- |
| SECURITY_ANONYMOUS<br><br>0x00000000      | The application-requested impersonation level is Anonymous.      |
| SECURITY_IDENTIFICATION<br><br>0x00000001 | The application-requested impersonation level is Identification. |
| SECURITY_IMPERSONATION<br><br>0x00000002  | The application-requested impersonation level is Impersonation.  |
| SECURITY_DELEGATION<br><br>0x00000003     | The application-requested impersonation level is Delegation.     |

##### Server Response Extensions

A successful response takes the following format. If the server receives more than one SMB_COM_NT_CREATE_ANDX request from a client before it sends back any response, then the server can respond to these requests in any order.

When a client requests extended information, then the response takes the form described below. Aside from the **WordCount**, **ResourceType**, **NMPipeStatus_or_FileStatusFlags**, **FileId**, **VolumeGUID**, **FileId**, **MaximalAccessRights**, and **GuestMaximalAccessRights** fields, all other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.64.2.

- SMB_Parameters
- {
- UCHAR WordCount;
- Words
- {
- UCHAR AndXCommand;
- UCHAR AndXReserved;
- USHORT AndXOffset;
- UCHAR OplockLevel;
- USHORT FID;
- ULONG CreateDisposition;
- FILETIME CreateTime;
- FILETIME LastAccessTime;
- FILETIME LastWriteTime;
- FILETIME LastChangeTime;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- LARGE_INTERGER AllocationSize;
- LARGE_INTERGER EndOfFile;
- USHORT ResourceType;
- USHORT NMPipeStatus_or_FileStatusFlags;
- UCHAR Directory;
- GUID VolumeGUID;
- ULONGLONG FileId;
- ACCESS_MASK MaximalAccessRights;
- ACCESS_MASK GuestMaximalAccessRights;
- }
- }
- SMB_Data
- {
- USHORT ByteCount;
- }

**SMB_Parameters:**

**WordCount (1 bytes):** This field SHOULD[&lt;49&gt;](#Appendix_A_49) be 0x2A.

| 0                        | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                               | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6          | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4                 | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------ | --- | --- | --- | --- | --- | --- | --- | ------------------------------- | --- | ---------- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | ---------- | --- | --- | --- | ----------------- | --- | --- | --- | --- | --- | ---------- | --- |
| AndXCommand              |     |     |     |     |     |     |     | AndXReserved                    |     |            |     |     |     |     |     | AndXOffset |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| OplockLevel              |     |     |     |     |     |     |     | FID                             |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | CreateDisposition |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | CreateTime        |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | LastAccessTime    |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | LastWriteTime     |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | LastChangeTime    |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | ExtFileAttributes |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | AllocationSize    |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | EndOfFile         |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | ResourceType      |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     | NMPipeStatus_or_FileStatusFlags |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     | Directory         |     |     |     |     |     |            |     |
| VolumeGUID (16 bytes)    |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| FileId                   |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| MaximalAccessRights      |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |
| GuestMaximalAccessRights |     |     |     |     |     |     |     |                                 |     |            |     |     |     |     |     |            |     |     |     |            |     |     |     |                   |     |     |     |     |     |            |     |

**ResourceType (2 bytes):** The file type. This field MUST be interpreted as follows:

| Name & value                          | Meaning                 |
| ------------------------------------- | ----------------------- |
| FileTypeDisk<br><br>0x0000            | File or Directory       |
| FileTypeByteModePipe<br><br>0x0001    | Byte mode named pipe    |
| FileTypeMessageModePipe<br><br>0x0002 | Message mode named pipe |
| FileTypePrinter<br><br>0x0003         | Printer Device          |

**NMPipeStatus_or_FileStatusFlags (2 bytes):** A union between the **NMPipeStatus** field and the new **FileStatusFlags** field. If the **ResourceType** field is a named pipe (**FileTypeByteModePipe** or **FileTypeMessageModePipe**), then this field MUST be the **NMPipeStatus** field:

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6               | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NMPipeStatus |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | FileStatusFlags |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**NMPipeStatus (2 bytes):** A 16-bit field that shows the status of the opened named pipe. This field is formatted as an SMB_NMPIPE_STATUS (\[MS-CIFS\] section 2.2.1.3).

If the **ResourceType** field is FileTypeDisk, then this field MUST be the **FileStatusFlags** field:

**FileStatusFlags (2 bytes):** A 16-bit field that shows extra information about the opened file or directory. Any combination of the following flags is valid. Unused bit fields SHOULD be set to zero by the server and MUST be ignored by the client.

| Name & bitmask              | Meaning                                                                    |
| --------------------------- | -------------------------------------------------------------------------- |
| NO_EAS<br><br>0x0001        | The file or directory has no extended attributes.                          |
| NO_SUBSTREAMS<br><br>0x0002 | The file or directory has no data streams other than the main data stream. |
| NO_REPARSETAG<br><br>0x0004 | The file or directory is not a reparse point.                              |

For all other values of **ResourceType**, this field SHOULD be set to zero by the server when sending a response and MUST be ignored when received by the client.

**VolumeGUID (16 bytes):** This field MUST be a GUID value that uniquely identifies the volume on which the file resides. This field MUST zero if the underlying file system does not support volume GUIDs.[&lt;50&gt;](#Appendix_A_50)

**FileId (8 bytes):** This field MUST be a 64-bit opaque value that uniquely identifies this file on a volume. This field MUST be set to zero if the underlying file system does not support unique FileId numbers on a volume. If the underlying file system does support unique FileId numbers, then this value SHOULD[&lt;51&gt;](#Appendix_A_51) be set to the unique FileId for this file.

**MaximalAccessRights (4 bytes):** The maximum access rights that the user opening the file has been granted for this file open. This field MUST be encoded in an ACCESS_MASK format, as specified in section [2.2.1.4](#Section_6e848af95cb64e7383acb68698e3d920).

**GuestMaximalAccessRights (4 bytes):** The maximum access rights that the guest account has when opening this file. This field MUST be encoded in an ACCESS_MASK format, as specified in section 2.2.1.4. Note that the notion of a guest account is implementation-specific[&lt;52&gt;](#Appendix_A_52). Implementations that do not support the notion of a guest account MUST set this field to zero.

**SMB_Data:**

**ByteCount (2 bytes):** This field SHOULD[&lt;53&gt;](#Appendix_A_53) be zero.

#### SMB_COM_SEARCH (0x81) Extensions

The SMB_COM_SEARCH request is sent by a client to search a directory for files or other objects on the server that have names matching a given wildcard template. The general format of the SMB_COM_SEARCH command requests and responses are given in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.58.

### Transaction Subcommand Extensions

#### TRANS_RAW_READ_NMPIPE (0x0011)

The TRANS_RAW_READ_NMPIPE subcommand allows for a [**raw read**](#gt_9900d904-169e-4292-8613-714e7c177641) of data from a named pipe. This method of reading data from a named pipe ignores message boundaries even if the pipe was set up as a message mode pipe.

The status of this subcommand is **obsolescent**.[&lt;54&gt;](#Appendix_A_54) Aside from its new status as obsolescent, this subcommand is exactly as described in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.5.2.

#### TRANS_CALL_NMPIPE (0x0054)

The TRANS_CALL_NMPIPE subcommand allows a client to connect to a named pipe, write to the named pipe, read from the named pipe, and close the named pipe in a single command.

The status of this subcommand is obsolescent.[&lt;55&gt;](#Appendix_A_55) Aside from its new status as obsolescent, this subcommand is exactly as described in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.5.11.

### Transaction 2 Subcommand Extensions

#### TRANS2_FIND_FIRST2 (0x0001)

##### Client Request Extensions

A TRANS2_FIND_FIRST2 subcommand of [SMB_COM_TRANSACTION2](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) is sent by the client to retrieve an enumeration of files, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.2.1.

The list of valid Information Level values has been extended, as specified in section [2.2.2.3.1](#Section_34b83ded0f52406f887a83fe89f33e23), to include SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO and SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO. Extensions are also presented to the SMB_FIND_FILE_BOTH_DIRECTORY_INFO Information Level, as specified in section [2.2.8.1.1](#Section_03d05a6fbbaf4a9ea556036581b02737). This Information Level now provides support for accessing enumerations of available previous version timestamps of files or directories.

Aside from the Information Level extensions and the **FileName** field, all of the other fields are as specified in \[MS-CIFS\] [2.2.6.2.1](#Section_d172d48c744649e4a76642f3688d8895).

**Trans2_Parameters**

**FileName (variable)**: This field is extended to support use of the @GMT token wildcard (section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af)).[&lt;56&gt;](#Appendix_A_56) If this character sequence contains the @GMT-\* wildcard, **Trans2_Data.InformationLevel** SHOULD be set to SMB_FIND_FILE_BOTH_DIRECTORY_INFO.[&lt;57&gt;](#Appendix_A_57)

**Trans2_Data**

**InformationLevel (2 bytes)**: This field contains an Information Level code, which determines the information contained in the response. The list of valid Information Level codes is specified in section 2.2.2.3.1. A client that has not negotiated long names support MUST request only SMB_INFO_STANDARD. If a client that has not negotiated long names support requests an **InformationLevel** other than SMB_INFO_STANDARD, then the server MUST return a status of STATUS_INVALID_PARAMETER (ERRDOS/ERRinvalidparam).

##### Server Response Extensions

A server MUST send a TRANS2_FIND_FIRST2 response in reply to a client TRANS2_FIND_FIRST2 subcommand request when the request is successful, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.2.2.

Thus subcommand also supports two new Information Levels, as well as extensions to the SMB_FIND_FILE_BOTH_DIRECTORY_INFO Information Level, as defined in section [2.2.2.3.1](#Section_34b83ded0f52406f887a83fe89f33e23). The format of the file information returned for these two Information Levels (and the format of information for a TRANS2_FIND_FIRST2 response for the special FileName pattern @GMT token) is listed in section [2.2.8.1](#Section_86ff792f35a74e668005604f8478ccea).If a client does not support long names (that is, SMB_FLAGS2_KNOWS_LONG_NAMES is not set in the **Flags2** field of the SMB Header), then any TRANS2_FIND_FIRST2 request with an Information Level other than SMB_INFO_STANDARD, or any TRANS2_FIND_NEXT2 request with an Information Level other than SMB_INFO_STANDARD or SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO, MUST be failed with STATUS_INVALID_PARAMETER.

#### TRANS2_FIND_NEXT2 (0x0002)

##### Client Request Extensions

The TRANS2_FIND_NEXT2 subcommand of the [SMB_COM_TRANSACTION2](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) request is sent by a client to continue a file enumeration, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.3.1. The request format is identical to the request format that is specified in \[MS-CIFS\] section 2.2.6.3.1, except that two new Information Levels have been added and one has been extended. See section 2.2.6.1.1 for details.

##### Server Response Extensions

The server MUST send a TRANS2_FIND_NEXT2 response in reply to a client [TRANS2_FIND_NEXT2](#Section_d172d48c744649e4a76642f3688d8895) subcommand request when the request is successful. The format of this packet is identical to what is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.3.2, except that two new Information Levels have been added and one has been extended. See section [2.2.6.1.2](#Section_2a812c92fa1a44bcb70f65b976f95556) for details.

#### TRANS2_QUERY_FS_INFORMATION (0x0003)

This subcommand supports new pass-through Information Level capabilities, as specified in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

##### Client Request Extensions

A TRANS2_QUERY_FS_INFORMATION subcommand of the [SMB_COM_TRANSACTION2](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) is sent by the client to request attribute information about the file system, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section [2.2.6.4.1](#Section_cf5f012fe1c4499d9df8a95add99221d).

##### Server Response Extensions

A server MUST send a TRANS2_QUERY_FS_INFORMATION response in reply to an SMB_COM_TRANSACTION2 client request with a [TRANS2_QUERY_FS_INFORMATION](#Section_f10c0034ded34ce2bccf485431037122) subcommand when the request is successful, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section [2.2.6.4.2](#Section_7ea947da5ce04a4b8789cb07a7f010e1).

#### TRANS2_SET_FS_INFORMATION (0x0004)

The TRANS2_SET_FS_INFORMATION subcommand of the SMB_COM_TRANSACTION2 command (section [2.2.4.4](#Section_714bb6fa7fab4dab8ff88a01c273b9ce)) is sent by the client to set file system attribute information on the server. This subcommand was introduced in the LAN Manager 2.0 dialect.[&lt;58&gt;](#Appendix_A_58)

The TRANS2_SET_FS_INFORMATION request and response formats are special cases of SMB_COM_TRANSACTION2 command. Only the TRANS2_SET_FS_INFORMATION specifics are described here.

##### Client Request

**SMB_Parameters:**

**WordCount (1 byte):** This field MUST be 0x0F.

**Words (30 bytes):**

**TotalParameterCount (2 bytes):** This field MUST be 0x0004.

**TotalDataCount (2 bytes):** This field MUST be greater than or equal to 0x0000.

**SetupCount (1 byte):** This field MUST be 0x01.

**Setup (2 bytes):** This field MUST be TRANS2_SET_FS_INFORMATION (0x0004).

**Trans2_Parameters**

- Trans2_Parameters
- {
- USHORT FID;
- USHORT InformationLevel;
- }

| 0   | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ---------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| FID |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | InformationLevel |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**FID (2 bytes):** A valid Fid that represents the open file on the server that is to have its file attributes changed.

**InformationLevel (2 bytes):** The Information Level of the request. This field MUST be a pass-through Information Level, as described in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

**Trans2_Data**

The Trans2_Data block carries the structure of the Information Level specified by the request's **Trans2_Parameters.InformationLevel** field. Because this subcommand only accepts pass-through Information Levels, the structure of this section is implementation specific.

##### Server Response

A server MUST send a TRANS2_SET_FS_INFORMATION response in reply to a client TRANS2_SET_FS_INFORMATION subcommand request when the request is successful. The server MUST set an error code in the **Status** field of the SMB header of the response to indicate whether the request was successful or failed. The server MUST respond with STATUS_NOT_SUPPORTED if the information level of the request is not a pass-through information level.

**Trans2_Parameters**

No Trans2_Parameters are sent by this message.

**Trans2_Data**

No Trans2_Data is sent by this message.

**Error Codes**

| SMB error class         | SMB error code                                                  | NT status code                           | POSIX equivalent                                                                                                                                                                              | Description                                        |
| ----------------------- | --------------------------------------------------------------- | ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| ERRDOS(0x01)            | ERRnoaccess(0x0005)                                             | STATUS_ACCESS_DENIED(0xC0000022)         | EPERM                                                                                                                                                                                         | Access denied.                                     |
| ERRbadfid(0x0006)       | STATUS_INVALID_HANDLE(0xC0000008)                               | ENOENT                                   | The Fid supplied is invalid.                                                                                                                                                                  |
| ERRnomem(0x0008)        | STATUS_INSUFF_SERVER_RESOURCES(0xC0000205)                      | ENOMEM                                   | The server is out of resources.                                                                                                                                                               |
| ERRunsup(0x0032)        | STATUS_NOT_SUPPORTED(0xC00000BB)                                |                                          | The supplied Information Level, as indicated by the Trans2_Parameters.InformationLevel value, is not a valid pass-through Information Level.                                                  |
| ERRinvalidparam(0x0057) | STATUS_INVALID_PARAMETER(0xC000000D)                            |                                          | The supplied pass-through Information Level values in the Trans2_Data block contain at least one invalid parameter.<br><br>OR<br><br>Server does not support pass-through Information Levels. |
| ERRSRV(0x02)            | ERRerror(0x0001)                                                | STATUS_INVALID_SMB(0x00010002)           |                                                                                                                                                                                               | Invalid SMB. Not enough parameter bytes were sent. |
| ERRinvtid(0x0005)       | STATUS_INVALID_HANDLE(0xC0000008)STATUS_SMB_BAD_TID(0x00050002) |                                          | The TID is no longer valid.                                                                                                                                                                   |
| ERRbaduid(0x005B)       | STATUS_INVALID_HANDLE(0xC0000008)STATUS_SMB_BAD_UID(0x005B0002) |                                          | The UID supplied is not known to the session.                                                                                                                                                 |
| ERRHRD(0x03)            | ERRnowrite(0x0013)                                              | STATUS_MEDIA_WRITE_PROTECTED(0xC00000A2) |                                                                                                                                                                                               | The Fid supplied is on write-protected media.      |
| ERRdata(0x0017)         | STATUS_DATA_ERROR(0xC000003E)                                   | EIO                                      | Disk I/O error.                                                                                                                                                                               |

#### TRANS2_QUERY_PATH_INFORMATION (0x0005)

This subcommand supports new pass-through Information Level capabilities, as specified in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

##### Client Request Extensions

A TRANS2_QUERY_PATH_INFORMATION subcommand of SMB_COM_TRANSACTION2 is sent by the client to request attribute information for a file or directory by specifying the path, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b), section 2.2.6.6.1.

##### Server Response Extensions

A server MUST send a TRANS2_QUERY_PATH_INFORMATION response in reply to an SMB_COM_TRANSACTION2 client request with a TRANS2_QUERY_PATH_INFORMATION subcommand when the request is successful, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.6.2.

#### TRANS2_SET_PATH_INFORMATION (0x0006)

This subcommand supports new pass-through Information Level capabilities, as specified in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

##### Client Request Extensions

A TRANS2_SET_PATH_INFORMATION subcommand of SMB_COM_TRANSACTION2 is sent by the client to request a change of attribute information for a file or directory by path, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.7.1.

##### Server Response Extensions

A server MUST send a TRANS2_SET_PATH_INFORMATION response in reply to an [SMB_COM_TRANSACTION2 (section 2.2.4.4)](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) client request with a TRANS2_SET_PATH_INFORMATION subcommand when the request is successful,as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.7.2.

#### TRANS2_QUERY_FILE_INFORMATION (0x0007)

This subcommand supports new pass-through Information Level capabilities, as specified in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

##### Client Request Extensions

A TRANS2_QUERY_FILE_INFORMATION subcommand of [SMB_COM_TRANSACTION2](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) is sent by a client to request attribute information for a file or directory that has been opened, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.8.1.

##### Server Response Extensions

A server MUST send a TRANS2_QUERY_PATH_INFORMATION response in reply to an SMB_COM_TRANSACTION2 client request with a TRANS2_QUERY_PATH_INFORMATION subcommand when the request is successful. The Trans2_Data block of the transaction response contains the information requested by the Information Level in the request, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.8.2.

#### TRANS2_SET_FILE_INFORMATION (0x0008)

This subcommand supports new pass-through Information Level capabilities, as specified in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475).

##### Client Request Extensions

A TRANS2_SET_FILE_INFORMATION subcommand of [SMB_COM_TRANSACTION2](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) is sent by the client to request a change of attribute information for a file or directory that has been opened, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.9.1.

##### Server Response Extensions

A server MUST send a TRANS2_SET_FILE_INFORMATION response in reply to an SMB_COM_TRANSACTION2 client request with a TRANS2_SET_FILE_INFORMATION subcommand when the request is successful, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.9.2.

### NT Transact Subcommand Extensions

#### NT_TRANSACT_CREATE (0x0001) Extensions

##### Client Request Extensions

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) command with an NT_TRANSACT_CREATE subcommand is sent by a client to open a file or device on the server. The NT_TRANSACT_CREATE subcommand is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.1. This extension adds the following:

- An additional flag bit is added to the **Flags** field. The additional flag, NT_CREATE_REQUEST_EXTENDED_RESPONSE, is used to request an extended response from the server.
- An additional parameter value, SECURITY_DELEGATION, is added to the **ImpersonationLevel** field.

All other fields are as specified in \[MS-CIFS\] section 2.2.7.1.

- NT_Trans_Parameters
- {
- ULONG Flags;
- ULONG RootDirectoryFID;
- ULONG DesiredAccess;
- LARGE_INTEGER AllocationSize;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- ULONG ShareAccess;
- ULONG CreateDisposition;
- ULONG CreateOptions;
- ULONG SecurityDescriptorLength;
- ULONG EALength;
- ULONG NameLength;
- ULONG ImpersonationLevel;
- UCHAR SecurityFlags;
- UCHAR Name\[NameLength\];
- }
- NT_Trans_Data
- {
- SECURITY_DESCRIPTOR SecurityDescriptor;
- FILE_FULL_EA_INFORMATION ExtendedAttributes\[\];
- }

**NT_Trans_Parameters (variable):**

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6               | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| Flags                     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| RootDirectoryFID          |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| DesiredAccess             |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AllocationSize (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ExtFileAttributes         |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ShareAccess               |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreateDisposition         |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreateOptions             |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SecurityDescriptorLength  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EALength                  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NameLength                |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ImpersonationLevel        |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SecurityFlags             |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | Name (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**Flags (4 bytes):** A 32-bit field containing a set of flags that modify the client request. Unused bits SHOULD be set to 0 by the client when sending a message and MUST be ignored when received by the server.

| Name & bitmask                                        | Meaning                                            |
| ----------------------------------------------------- | -------------------------------------------------- |
| NT_CREATE_REQUEST_OPLOCK<br><br>0x00000002            | Level I (exclusive) OpLock requested.              |
| NT_CREATE_REQUEST_OPBATCH<br><br>0x00000004           | Batch OpLock requested.                            |
| NT_CREATE_OPEN_TARGET_DIR<br><br>0x00000008           | Parent directory of the target is to be opened.    |
| NT_CREATE_REQUEST_EXTENDED_RESPONSE<br><br>0x00000010 | Extended information is requested in the response. |

**ImpersonationLevel (4 bytes):** This field specifies the impersonation level requested by the application that is issuing the create request, and MUST contain one of the following values. The server MUST validate this field but otherwise ignore it.

Impersonation is described in [\[MS-WPO\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-WPO%5d.pdf#Section_c5f54a7765be40a0bb829e4181d8ab67) section 9.7; for more information about impersonation, see [\[MSDN-IMPERS\]](http://go.microsoft.com/fwlink/?LinkId=106009).

| Value                                     | Meaning                                                          |
| ----------------------------------------- | ---------------------------------------------------------------- |
| SECURITY_ANONYMOUS<br><br>0x00000000      | The application-requested impersonation level is Anonymous.      |
| SECURITY_IDENTIFICATION<br><br>0x00000001 | The application-requested impersonation level is Identification. |
| SECURITY_IMPERSONATION<br><br>0x00000002  | The application-requested impersonation level is Impersonation.  |
| SECURITY_DELEGATION<br><br>0x00000003     | The application-requested impersonation level is Delegation.     |

##### Server Response Extensions

When a client requests extended information by setting NT_CREATE_REQUEST_EXTENDED_RESPONSE, a successful response takes the following format.

Aside from **ResponseType**, **NMPipeStatus_or_FileStatusFlags**, **VolumeGUID**, **FileId**, **MaximalAccessRights**, and **GuestMaximalAccessRights** fields, all other fields are as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.1.2.

- NT_Trans_Parameters
- {
- UCHAR OpLockLevel;
- UCHAR ResponseType;
- USHORT FID;
- ULONG CreateAction;
- ULONG EAErrorOffset;
- FILETIME CreationTime;
- FILETIME LastAccessTime;
- FILETIME LastWriteTime;
- FILETIME LastChangeTime;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- LARGE_INTEGER AllocationSize;
- LARGE_INTEGER EndOfFile;
- USHORT ResourceType;
- SMB_NMPIPE_STATUS NMPipeStatus_or_FileStatusFlags;
- UCHAR Directory;
- GUID VolumeGUID;
- ULONGLONG FileId;
- ACCESS_MASK MaximalAccessRights;
- ACCESS_MASK GuestMaximalAccessRights;
- }

**NT_Trans_Parameters (69 bytes):**

| 0                         | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8                        | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                               | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------- | --- | --- | --- | --- | --- | --- | --- | ------------------------ | --- | ---------- | --- | --- | --- | --- | --- | ------------------------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| OpLockLevel               |     |     |     |     |     |     |     | ResponseType             |     |            |     |     |     |     |     | FID                             |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreateAction              |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EAErrorOffset             |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreationTime (variable)   |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastAccessTime (variable) |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastWriteTime (variable)  |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastChangeTime (variable) |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ExtFileAttributes         |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AllocationSize (variable) |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EndOfFile (variable)      |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ResourceType              |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     | NMPipeStatus_or_FileStatusFlags |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Directory                 |     |     |     |     |     |     |     | VolumeGUID (16 bytes)    |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     | FileId                   |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |                          |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     | MaximalAccessRight       |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     | GuestMaximalAccessRights |     |            |     |     |     |     |     |                                 |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                       |     |     |     |     |     |     |     |

**ResponseType (1 byte):** The response type received. This field MUST be set to any one of the following values, based on the response type.

| Name & bitmask                    | Meaning                                        |
| --------------------------------- | ---------------------------------------------- |
| NON_EXTENDED_RESPONSE<br><br>0x00 | Response received is not an extended response. |
| EXTENDED_RESPONSE<br><br>0x01     | Response received is an extended response.     |

**NMPipeStatus_or_FileStatusFlags (2 bytes):** A union between the **NMPipeStatus** field and the new **FileStatusFlags** field. If the **ResourceType** field is a named pipe (**FileTypeByteModePipe** or **FileTypeMessageModePipe**), then this field MUST be the **NMPipeStatus** field:

| 0            | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6               | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------ | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NMPipeStatus |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | FileStatusFlags |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**NMPipeStatus (2 bytes):** A 16-bit field that shows the status of the opened named pipe. This field is formatted as an SMB_NMPIPE_STATUS (\[MS-CIFS\] section 2.2.1.3).

If the **ResourceType** field is FileTypeDisk, then this field MUST be the **FileStatusFlags** field.

**FileStatusFlags (2 bytes):** A 16-bit field that shows extra information about the opened file or directory. Any combination of the following flags is valid. Unused bit fields SHOULD be set to zero by the server and MUST be ignored by the client.

| Name & bitmask              | Meaning                                                                    |
| --------------------------- | -------------------------------------------------------------------------- |
| NO_EAS<br><br>0x0001        | The file or directory has no extended attributes.                          |
| NO_SUBSTREAMS<br><br>0x0002 | The file or directory has no data streams other than the main data stream. |
| NO_REPARSETAG<br><br>0x0004 | The file or directory is not a reparse point.                              |

For all other values of **ResourceType**, this field SHOULD be set to zero by the server when sending a response and MUST be ignored when received by the client.

**VolumeGUID (16 bytes):** This field MUST be a GUID value that uniquely identifies the volume on which the file resides. This field MUST be zero if the underlying file system does not support volume GUIDs.[&lt;59&gt;](#Appendix_A_59)

**FileId (8 bytes):** This field MUST be a 64-bit opaque value that uniquely identifies this file on a volume. This field MUST be set to zero if the underlying file system does not support unique **FileId** numbers on a volume. If the underlying file system does support unique **FileId** numbers, then this value SHOULD[&lt;60&gt;](#Appendix_A_60) be set to the unique **FileId** for this file.

**MaximalAccessRight (4 bytes):** The maximum access rights that the user opening the file has been granted for this file open. This field MUST be encoded in an ACCESS_MASK format, as specified in section [2.2.1.4](#Section_6e848af95cb64e7383acb68698e3d920).

**GuestMaximalAccessRights (4 bytes):** The maximum access rights that the guest account has when opening this file. This field MUST be encoded in an ACCESS_MASK format, as specified in section 2.2.1.4. Note that the notion of a guest account is implementation-specific[&lt;61&gt;](#Appendix_A_61). Implementations that do not support the notion of a guest account MUST set this field to zero.

#### NT_TRANSACT_IOCTL (0x0002)

An SMB_COM_NT_TRANSACT (section [2.2.4.8](#Section_602802fd5870433a955c79897847053e)) command with an NT_TRANSACT_IOCTL subcommand is sent by a client to pass an IOCTL or file system control (FSCTL) command to a server. The NT_TRANSACT_IOCTL subcommand is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.2.

##### Client Request Extensions

The NT_TRANSACT_IOCTL request is a special case of the SMB_COM_NT_TRANSACT command request. Only the NT_TRANSACT_IOCTL specifics are described here.

The FSCTL operations listed in the table below are new to these extensions and only those are specific to the SMB protocol.[&lt;62&gt;](#Appendix_A_62) Other FSCTL and IOCTL control codes are not defined in this specification and are specific to the underlying object store of the server.[&lt;63&gt;](#Appendix_A_63) If an application requests an undefined FSCTL or IOCTL operation, then the client SHOULD still pass the request through to the server.

| Name                          | Value      | Description                                                                                  |
| ----------------------------- | ---------- | -------------------------------------------------------------------------------------------- |
| FSCTL_SRV_ENUMERATE_SNAPSHOTS | 0x00144064 | Enumerate previous versions of a file.                                                       |
| FSCTL_SRV_REQUEST_RESUME_KEY  | 0x00140078 | Retrieve an opaque file reference for server-side data movement.[&lt;64&gt;](#Appendix_A_64) |
| FSCTL_SRV_COPYCHUNK           | 0x001440F2 | Perform server-side data movement.[&lt;65&gt;](#Appendix_A_65)                               |

FSCTL_SRV_ENUMERATE_SNAPSHOTS Request

This FSCTL is used to enumerate available previous version timestamps (snapshots) of a file or directory.

The FSCTL_SRV_ENUMERATE_SNAPSHOTS request format is a special case of the NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_ENUMERATE_SNAPSHOTS request specifics are described here.

**SMB_Parameters**

**WordCount (1 byte):** The value of this field MUST be 0x17.

**Words (46 bytes):** As specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.62.1 and with the following exceptions:

**MaxDataCount (4 bytes):** This field MUST be greater than or equal to 0x000C.

**SetupCount (1 byte):** The number of setup words that are included in the transaction request. The value MUST be set to 0x04.

**Setup (8 bytes):** As specified in \[MS-CIFS\] section 2.2.7.2.1 and with the following exceptions:

**FunctionCode (4 bytes):** This field MUST be set to 0x00144064.

**FID (2 bytes):** This field MUST contain a valid Fid representing a valid Open on a file. This is the file for which snapshots are being requested.

**IsFsctl (1 byte):** MUST be TRUE (any non-zero value).

**IsFlags (1 byte):** MUST be zero (0x00).

**NT_Trans_Parameters**

No NT Trans parameters are sent in this request.

**NT_Trans_Data**

No NT Trans data is sent in this request.

FSCTL_SRV_REQUEST_RESUME_KEY Request

This FSCTL is used to retrieve an opaque file reference for server-side data movement operations, as specified in section [3.2.4.11.2](#Section_bceed388009a4aa688543a06c989af64).

The FSCTL_SRV_REQUEST_RESUME_KEY request format is a special case of the NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_REQUEST_RESUME_KEY request specifics are described here.

**SMB_Parameters**

**WordCount (1 byte):** The value of this field MUST be 0x17.

**Words (46 bytes):**

**MaxDataCount (4 bytes):** This field MUST be greater than or equal to 0x001D.

**Setup (8 bytes):**

**FunctionCode (4 bytes):** This field MUST be 0x00140078.

**FID (2 bytes):** This field MUST contain a valid Fid that represents a valid Open on a file. This file is the source file for a server-side data copy operation.

**IsFsctl (1 byte):** MUST be TRUE (any non-zero value).

**IsFlags (1 byte):** MUST be zero (0x00).

**NT_Trans_Parameters**

No NT Trans parameters are sent in this request.

**NT_Trans_Data**

No NT Trans data is sent in this request.

FSCTL_SRV_COPYCHUNK Request

This FSCTL is used for server-side data movement, as specified in section 3.2.4.11.2.

The FSCTL_SRV_COPYCHUNK request format is a special case of NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_COPYCHUNK request specifics are described here.

**SMB_Parameters**

**WordCount (1 byte):** The value of this field MUST be 0x17.

**Words (46 bytes):**

**TotalDataCount (4 bytes):** This field MUST be greater than or equal to 0x0034.

**MaxDataCount (4 bytes):** This field MUST be greater than or equal to 0x001D.

**Setup (8 bytes):**

**FunctionCode (4 bytes):** This field MUST be 0x00144078.

**FID (2 bytes):** This field MUST contain a valid Fid that represents a valid Open on a file. This file is the destination file for a server-side data copy operation.

**IsFsctl (1 byte):** This field MUST be TRUE (any non-zero value).

**IsFlags (1 byte):** The value of this field MUST be zero (0x00).

**NT_Trans_Parameters**

No NT Trans parameters are sent in this request.

**NT_Trans_Data**

- NT_Trans_Data
- {
- COPYCHUNK_RESUME_KEY CopychunkResumeKey;
- ULONG ChunkCount;
- ULONG Reserved;
- SRV_COPYCHUNK CopychunkList\[ChunkCount\];
- }

| 0                             | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| CopychunkResumeKey (24 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ChunkCount                    |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved                      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CopychunkList (variable)      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**CopychunkResumeKey (24 bytes):** An opaque 24-byte server copychunk resume key for a source file in a server-side file copy operation. This field value is received from a previous FSCTL_SRV_REQUEST_RESUME_KEY server response.

**ChunkCount (4 bytes):** The number of entries, or "copychunks", in the **CopyChunkList**. This field also represents the number of server-side data movement operations being requested. This field MUST NOT be zero.

**Reserved (4 bytes):** A reserved field. This field SHOULD be set to zero when sending the request. This field MUST be ignored by the server when the message is received.

**CopychunkList (variable):** A concatenated list of copychunks. Each entry is formatted as a SRV_COPYCHUNK structure.

###### SRV_COPYCHUNK

- SRV_COPYCHUNK
- {
- LARGE_INTEGER SourceOffset;
- LARGE_INTEGER DestinationOffset;
- ULONG CopyLength;
- ULONG Reserved;
- }

| 0                 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SourceOffset      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| DestinationOffset |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CopyLength        |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved          |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SourceOffset (8 bytes):** The offset, in bytes, into the source file from which data is being copied.

**DestinationOffset (8 bytes):** The offset, in bytes, into the destination file to which data is being copied.

**CopyLength (4 bytes):** The number of bytes to copy from the source file to the destination file.

**Reserved (4 bytes):** This field SHOULD[&lt;66&gt;](#Appendix_A_66) be set to zero by the client and MUST be ignored upon receipt by the server.

##### Server Response Extensions

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) response for an NT_TRANSACT_IOCTL ([\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.2) subcommand MUST be sent by a server in reply to a successful NT_TRANSACT_IOCTL request.

The NT_TRANSACT_IOCTL response is a special case of the SMB_COM_NT_TRANSACT command response. Only the NT_TRANSACT_IOCTL specifics are described here.

###### FSCTL_SRV_ENUMERATE_SNAPSHOTS Response

The FSCTL_SRV_ENUMERATE_SNAPSHOTS response format is a special case of the NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_ENUMERATE_SNAPSHOTS response specifics are described here.

**SMB_Parameters**

**WordCount (1 byte):** This field MUST be set to 0x16.

**SetupCount (1 byte):** The number of setup words that are included in the transaction response. The value MUST be set to 0x04.

**Setup (8 bytes):** This field contains the following:

**Function(2 bytes):** The transaction subcommand code, which is used to identify the operation performed by the server. The value MUST be set to 0x0002.

**FunctionCode (4 bytes):** This field MUST be set to 0x00144064.

**FID (2 bytes):** This field MUST contain a File ID representing the open of a file for which snapshots are requested.

**NT_Trans_Parameters**

No NT Trans parameters are sent in this response.

**NT_Trans_Data**

- NT_Trans_Data
- {
- ULONG NumberOfSnapShots;
- ULONG NumberOfSnapShotsReturned;
- ULONG SnapShotArraySize;
- WCHAR SnapShotMultiSZ\[\];
- }

| 0                          | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| -------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NumberOfSnapShots          |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NumberOfSnapShotsReturned  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SnapShotArraySize          |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| SnapShotMultiSZ (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                        |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**NumberOfSnapShots (4 bytes):** The number of snapshots that the underlying object store contains of this file.

**NumberOfSnapShotsReturned (4 bytes):** This value MUST be the number of snapshots that are returned in this response. If this value is less than **NumberofSnapshots**, then there are more snapshots than were able to fit in this response.

**SnapShotArraySize (4 bytes):** The length, in bytes, of the **SnapShotMultiSZ** field.

**SnapShotMultiSZ (variable):** A concatenated list of available snapshots. Each snapshot MUST be encoded as a NULL-terminated sequence of 16-bit Unicode characters, and MUST take on the following form: @GMT-YYYY.MM.DD-HH.MM.SS. The concatenated list MUST be terminated by one additional 16-bit Unicode NULL character. If the response contains no snapshots, then the server MUST set this field to two 16-bit Unicode NULL characters.

###### FSCTL_SRV_REQUEST_RESUME_KEY Response

The FSCTL_SRV_REQUEST_RESUME_KEY response format is a special case of the NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_REQUEST_RESUME_KEY response specifics are described here.

Although this FSCTL includes support for returning extended context information for a copychunk resume key, this feature is considered reserved but not implemented and is not used in this response.

**NT_Trans_Parameters**

No NT Trans parameters are sent in this response.

**NT_Trans_Data**

- NT_Trans_Data
- {
- COPYCHUNK_RESUME_KEY CopychunkResumeKey;
- ULONG ContextLength;
- UCHAR Context\[ContextLength\] (optional);
- }

| 0                             | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| CopychunkResumeKey (24 bytes) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ContextLength                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Context (variable)            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**CopychunkResumeKey (24 bytes):** A 24-byte copychunk resume key generated by the server that can be subsequently used by the client to uniquely identify the open source file in a FSCTL_SRV_COPYCHUNK request. The client MUST NOT attach any interpretation to this key and MUST treat it as an opaque value. For more information, see section [3.3.5.11.1](#Section_7187919879ea439b970b8c301128edce).

**ContextLength (4 bytes):** The length, in bytes, of the **Context** field. Since this feature is not used, this field SHOULD be set to zero by the server and MUST be ignored by the client.

**Context (variable):** The copychunk resume key's extended context information. Since this feature is not used, this field SHOULD[&lt;67&gt;](#Appendix_A_67) be zero bytes in length. The client MUST ignore it on receipt.

###### FSCTL_SRV_COPYCHUNK Response

The FSCTL_SRV_COPYCHUNK response format is a special case of NT_TRANSACT_IOCTL subcommand. Only the FSCTL_SRV_COPYCHUNK response specifics are described here.

**NT_Trans_Parameters**

No NT Trans parameters are sent in this response.

**NT_Trans_Data**

- NT_Trans_Data
- {
- ULONG ChunksWritten;
- ULONG ChunkBytesWritten;
- ULONG TotalBytesWritten;
- }

| 0                 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| ChunksWritten     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ChunkBytesWritten |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| TotalBytesWritten |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**ChunksWritten (4 bytes):** This field MUST represent the number of copychunk operations successfully processed by the server.

**ChunkBytesWritten (4 bytes):** This field is unused. This field MUST be set to zero by the server and MUST be ignored by the client.

**TotalBytesWritten (4 bytes):** This field MUST represent the total number of bytes written to the destination file across all copychunk operations.

#### NT_TRANSACT_SET_SECURITY_DESC (0x0003) Extensions

An SMB_COM_NT_TRANSACT command (section [2.2.4.8](#Section_602802fd5870433a955c79897847053e)) with an NT_TRANSACT_SET_SECURITY_DESC allows a client to set the security descriptors for a file or device on the server. The NT_TRANSACT_SET_SECURITY_DESC subcommand is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.3. This extension adds LABEL_SECURITY_INFORMATION, ATTRIBUTE_SECURITY_INFORMATION, SCOPE_SECURITY_INFORMATION, and BACKUP_SECURITY_INFORMATION parameter values to the **SecurityInformation** field.

**SecurityInformation (4 bytes)**: A ULONG. Fields of the security descriptor to be set. These values can be logically OR-ed together to set several descriptors in one request. Bits and security descriptors not mentioned in the following table MUST be ignored and MUST NOT be processed.

| Name and bitmask                                 | Meaning                                                                                 |
| ------------------------------------------------ | --------------------------------------------------------------------------------------- |
| OWNER_SECURITY_INFORMATION<br><br>0x00000001     | Owner of the object or resource.                                                        |
| GROUP_SECURITY_INFORMATION<br><br>0x00000002     | Group associated with the object or resource.                                           |
| DACL_SECURITY_INFORMATION<br><br>0x00000004      | DACL associated with the object or resource.                                            |
| SACL_SECURITY_INFORMATION<br><br>0x00000008      | SACL associated with the object or resource.                                            |
| LABEL_SECURITY_INFORMATION<br><br>0x00000010     | Integrity label in the security descriptor of the file or named pipe.                   |
| ATTRIBUTE_SECURITY_INFORMATION<br><br>0x00000020 | Resource attribute in the security descriptor of the file or named pipe.                |
| SCOPE_SECURITY_INFORMATION<br><br>0x00000040     | Central access policy of resource in the security descriptor of the file or named pipe. |
| BACKUP_SECURITY_INFORMATION<br><br>0x00010000    | Security descriptor information used for backup operation.                              |

#### NT_TRANSACT_QUERY_SECURITY_DESC (0x0006) Extensions

An SMB_COM_NT_TRANSACT command (section [2.2.4.8](#Section_602802fd5870433a955c79897847053e)) with an NT_TRANSACT_QUERY_SECURITY_DESC allows a client to retrieve the security descriptors for a file or device on the server. The NT_TRANSACT_QUERY_SECURITY_DESC subcommand is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.7.6. This extension adds LABEL_SECURITY_INFORMATION, ATTRIBUTE_SECURITY_INFORMATION, SCOPE_SECURITY_INFORMATION, and BACKUP_SECURITY_INFORMATION parameter values to the **SecurityInfoFields** field.

**SecurityInfoFields (4 bytes)**: A ULONG. This field represents the requested fields of the security descriptor to be retrieved. These values can be logically OR-ed together to request several descriptors in one request. The descriptor response format contains storage for all the descriptors. The values returned for security descriptors corresponding to bits not mentioned in the following table MUST be ignored.

| Name and bitmask                                 | Meaning                                                                                 |
| ------------------------------------------------ | --------------------------------------------------------------------------------------- |
| OWNER_SECURITY_INFORMATION<br><br>0x00000001     | Owner of the object or resource.                                                        |
| GROUP_SECURITY_INFORMATION<br><br>0x00000002     | Group associated with the object or resource.                                           |
| DACL_SECURITY_INFORMATION<br><br>0x00000004      | DACL associated with the object or resource.                                            |
| SACL_SECURITY_INFORMATION<br><br>0x00000008      | SACL associated with the object or resource.                                            |
| LABEL_SECURITY_INFORMATION<br><br>0x00000010     | Integrity label in the security descriptor of the file or named pipe.                   |
| ATTRIBUTE_SECURITY_INFORMATION<br><br>0x00000020 | Resource attribute in the security descriptor of the file or named pipe.                |
| SCOPE_SECURITY_INFORMATION<br><br>0x00000040     | Central access policy of resource in the security descriptor of the file or named pipe. |
| BACKUP_SECURITY_INFORMATION<br><br>0x00010000    | Security descriptor information used for backup operation.                              |

#### NT_TRANSACT_QUERY_QUOTA (0x0007)

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) command with an NT_TRANSACT_QUERY_QUOTA subcommand code is used by a client to query quota information for a user or multiple users. This subcommand is new to these extensions.

##### Client Request

The NT_TRANSACT_QUERY_QUOTA request is a special case of the SMB_COM_NT_TRANSACT command request. Only the NT_TRANSACT_QUERY_QUOTA specifics are described here.

At least one of **NT_Trans_Parameters.SidListLength** or **NT_Trans_Parameters.StartSidLength** MUST be zero. If both are zero, then the quota information for all [**SIDs**](#gt_83f2020d-0804-4840-a5ac-e06439d50f8d), as specified in [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2) section 2.4.2, on the underlying object store of a share MUST be enumerated by the server.

**SMB_Parameters**

**WordCount (1 byte):** The value of this field MUST be 0x13.

**Words (38 bytes):** As specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.62.1 and with the following exceptions:

**SetupCount (1 bytes):** This field MUST be 0x00.

**NT_Trans_Parameters:**

- NT_Trans_Parameters
- {
- USHORT FID;
- BOOLEAN ReturnSingleEntry;
- BOOLEAN RestartScan;
- ULONG SidListLength;
- ULONG StartSidLength;
- ULONG StartSidOffset;
- }

| 0              | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                 | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4           | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| -------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ----------------- | --- | --- | --- | ---------- | --- | --- | --- | ----------- | --- | --- | --- | --- | --- | ---------- | --- |
| FID            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | ReturnSingleEntry |     |     |     |            |     |     |     | RestartScan |     |     |     |     |     |            |     |
| SidListLength  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                   |     |     |     |            |     |     |     |             |     |     |     |     |     |            |     |
| StartSidLength |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                   |     |     |     |            |     |     |     |             |     |     |     |     |     |            |     |
| StartSidOffset |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                   |     |     |     |            |     |     |     |             |     |     |     |     |     |            |     |

**FID (2 bytes):** An Fid to a file or directory. The quota information of the object store underlying the file or directory MUST be queried.

**ReturnSingleEntry (1 byte):** If TRUE (any non-zero value), then this field indicates that the server behavior is to be restricted to only return a single SID's quota information instead of filling the entire buffer.

**RestartScan (1 byte):** If TRUE (any non-zero value), then this field indicates that the scan of quota information is to be restarted.

**SidListLength (4 bytes):** If non-zero, then this field indicates that the client is requesting quota information of a particular set of SIDs and MUST represent the length of the **NT_Trans_Data.SidList** field.

**StartSidLength (4 bytes):** If non-zero, then this field indicates that the **SidList** field contains a start SID (that is, a single SID entry indicating to the server where to start user quota information enumeration) and MUST represent the length, in bytes, of that **SidList** entry.

**StartSidOffset (4 bytes):** If **StartSidLength** is non-zero, then this field MUST represent the offset from the start of the NT_Trans_Data to the specific **SidList** entry at which to begin user quota information enumeration. Otherwise, this field SHOULD be set to zero and MUST be ignored by the server.

**NT_Trans_Data:**

- NT_Trans_Data
- {
- FILE_GET_QUOTA_INFORMATION SidList\[\] (optional);
- }

| 0                  | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------ | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| SidList (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**SidList (variable):** A list of one or more SIDs that are formatted as a FILE_GET_QUOTA_INFORMATION structure, as specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.4.33.1. If **SidListLength** is non-zero, then this field MUST contain a list of one or more SIDs for which quota information is being requested. If **StartSidLength** is non-zero, then this field MUST contain a start SID. If both **SidListLength** and **StartSidLength** are zero, then this field MUST NOT be included in the request.

##### Server Response

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) response for an NT_TRANSACT_QUERY_QUOTA subcommand MUST be sent by a server in reply to a client [NT_TRANSACT_QUERY_QUOTA](#Section_9f3f73f99c4a42ba9f56e6352491d714) subcommand request when the request is successful.

The NT_TRANSACT_QUERY_QUOTA response is a special case of the SMB_COM_NT_TRANSACT command response. Only the NT_TRANSACT_QUERY_QUOTA specifics are described here.

| 0                        | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------------ | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NT_Trans_Parameters      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| NT_Trans_Data (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**NT_Trans_Parameters (4 bytes):**

- NT_Trans_Parameters
- {
- ULONG DataLength;
- }

| 0          | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| DataLength |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**DataLength (4 bytes):** The length, in bytes, of the returned user quota information. This field MUST be equal to the **SMB_Parameters.Words.TotalDataCount** field.

**NT_Trans_Data (variable):**

- NT_Trans_Data
- {
- FILE_QUOTA_INFORMATION QuotaInformation\[\] (variable);
- }

| 0                           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| QuotaInformation (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                         |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**QuotaInformation (variable):** A concatenated list of FILE_QUOTA_INFORMATION structures, as specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.4.33.

**Error Codes**

| SMB error class | SMB error code          | NT status code                             | POSIX equivalent | Description                                                                                     |
| --------------- | ----------------------- | ------------------------------------------ | ---------------- | ----------------------------------------------------------------------------------------------- |
| ERRDOS(0x01)    | ERRbadfunc(0x0001)      | STATUS_INVALID_DEVICE_REQUEST(0xC0000008)  |                  | Quotas are not enabled on the volume.                                                           |
| ERRDOS(0x01)    | ERRbadfid(0x0006)       | STATUS_INVALID_HANDLE(0xC0000008)          | EBADF            | The Fid is invalid.                                                                             |
| ERRDOS(0x01)    | ERRnoaccess(0x0005)     | STATUS_ACCESS_DENIED(0xC0000022)           | EPERM            | Access denied.                                                                                  |
| ERRDOS(0x01)    | ERRinvalidparam(0x0057) | STATUS_INVALID_PARAMETER(0xC000000D)       |                  | A parameter is invalid.                                                                         |
| ERRDOS(0x01)    | ERRinvalidsid(0x0539)   | STATUS_INVALID_SID(0xC0000078)             |                  | The **StartSid** parameter did not contain a valid SID.                                         |
| ERRSRV(0x02)    | ERRerror(0x0001)        | STATUS_INVALID_SMB(0x00010002)             |                  | Invalid SMB. Byte count and sizes are inconsistent.                                             |
| ERRSRV(0x02)    | ERRinvtid(0x0005)       | STATUS_BAD_TID(0x00050002)                 |                  | The TID is no longer valid.                                                                     |
| ERRSRV(0x02)    | ERRnomem(0x0008)        | STATUS_INSUFF_SERVER_RESOURCES(0xC0000205) | ENOMEM           | The server is out of resources.                                                                 |
| ERRSRV(0x02)    | ERRbaduid(0x005B)       | STATUS_BAD_UID(0x005B0002)                 |                  | The UID supplied is not known to the session.                                                   |
| ERRSRV(0x02)    |                         | STATUS_QUOTA_LIST_INCONSISTENT(0xC0000266) |                  | The **SidList** parameter did not contain a valid, properly formed list.                        |
| ERRSRV(0x02)    | ERRmoredata(0x00EA)     | STATUS_BUFFER_OVERFLOW(0x80000005)         |                  | The number of bytes of changed data exceeded the MaxParameterCount field in the client request. |
| ERRHRD(0x03)    | ERRdata(0x0017)         | STATUS_DATA_ERROR(0xC000003E)              | EIO              | Disk I/O error.                                                                                 |
| ERRHRD(0x03)    | ERRnowrite(0x0013)      | STATUS_MEDIA_WRITE_PROTECTED(0xC00000A2)   | EROFS            | Attempt to modify a read-only file system.                                                      |

#### NT_TRANSACT_SET_QUOTA (0x0008)

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) request with an NT_TRANSACT_SET_QUOTA subcommand code is sent by a client to set user quota information on the underlying object store of a server. This subcommand is new to these extensions.

##### Client Request

The NT_TRANSACT_SET_QUOTA request is a special case of the SMB_COM_NT_TRANSACT command request. Only the NT_TRANSACT_SET_QUOTA specifics are described here.

**SMB_Parameters:**

**WordCount (1 byte):** The value of this field MUST be 0x13.

**Words (38 bytes):** As specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.4.62.1 and with the following exceptions:

**SetupCount (1 bytes):** This field MUST be 0x00.

| 0                   | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                        | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | ------------------------ | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NT_Trans_Parameters |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     | NT_Trans_Data (variable) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |                          |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**NT_Trans_Parameters (2 bytes):**

- NT_Trans_Parameters
- {
- USHORT FID;
- }

| 0   | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| FID |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |

**FID (2 bytes):** An Fid to a file or directory. The quota information of the object store underlying the file or directory MUST be modified.

**NT_Trans_Data (variable):**

- NT_Trans_Data
- {
- FILE_QUOTA_INFORMATION QuotaInformation\[\] (variable);
- }

| 0                           | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| --------------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| QuotaInformation (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                         |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**QuotaInformation (variable):** A concatenated list of FILE_QUOTA_INFORMATION structures, as specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.4.33.

##### Server Response

An [SMB_COM_NT_TRANSACT (section 2.2.4.8)](#Section_602802fd5870433a955c79897847053e) response for the NT_TRANSACT_SET_QUOTA subcommand MUST be sent by a server in reply to a client NT_TRANSACT_SET_QUOTA request when the request is successful.

The NT_TRANSACT_SET_QUOTA response is a special case of the SMB_COM_NT_TRANSACT command response. Only the NT_TRANSACT_SET_QUOTA specifics are described here.

**NT_Trans_Parameters**

No NT Trans parameters are returned in this response.

**NT_Trans_Data**

No NT Trans data is returned in this response.

**Error Codes**

| SMB error class | SMB error code          | NT status code                             | POSIX equivalent | Description                                                                |
| --------------- | ----------------------- | ------------------------------------------ | ---------------- | -------------------------------------------------------------------------- |
| ERRDOS(0x01)    | ERRbadfunc(0x0001)      | STATUS_INVALID_DEVICE_REQUEST(0xC0000008)  |                  | Quotas are not enabled on the volume.                                      |
| ERRDOS(0x01)    | ERRbadfid(0x0006)       | STATUS_INVALID_HANDLE(0xC0000008)          | EBADF            | The Fid is invalid.                                                        |
| ERRDOS(0x01)    | ERRnoaccess(0x0005)     | STATUS_ACCESS_DENIED(0xC0000022)           | EPERM            | Access denied.                                                             |
| ERRDOS(0x01)    | ERRinvalidparam(0x0057) | STATUS_INVALID_PARAMETER(0xC000000D)       |                  | A parameter is invalid.                                                    |
| ERRSRV(0x02)    | ERRerror(0x0001)        | STATUS_INVALID_SMB(0x00010002)             |                  | Invalid SMB. Byte count and sizes are inconsistent.                        |
| ERRSRV(0x02)    | ERRinvtid(0x0005)       | STATUS_BAD_TID(0x00050002)                 |                  | The TID is no longer valid.                                                |
| ERRSRV(0x02)    | ERRnomem(0x0008)        | STATUS_INSUFF_SERVER_RESOURCES(0xC0000205) | ENOMEM           | The server is out of resources.                                            |
| ERRSRV(0x02)    | ERRbaduid(0x005B)       | STATUS_BAD_UID(0x005B0002)                 |                  | The UID supplied is not known to the session.                              |
| ERRSRV(0x02)    |                         | STATUS_QUOTA_LIST_INCONSISTENT(0xC0000266) |                  | The _Sid_ parameter in FILE_QUOTA_INFORMATION did not contain a valid SID. |
| ERRHRD(0x03)    | ERRdata(0x0017)         | STATUS_DATA_ERROR(0xC000003E)              | EIO              | Disk I/O error.                                                            |
| ERRHRD(0x03)    | ERRnowrite(0x0013)      | STATUS_MEDIA_WRITE_PROTECTED(0xC00000A2)   | EROFS            | Attempt to modify a read-only file system.                                 |

### Information Levels

In addition to the specification in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.8, the client MUST map the application provided FSCC information levels to SMB information levels as specified below.

FIND Information Levels

| FSCC Level                     | SMB Level                            |
| ------------------------------ | ------------------------------------ |
| FileIdFullDirectoryInformation | SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO |
| FileIdBothDirectoryInformation | SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO |

#### FIND Information Level Extensions

##### SMB_FIND_FILE_BOTH_DIRECTORY_INFO Extensions

If the query being executed is a request for the enumeration of available previous versions (section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af)), then the returned structure is identical to the structure that is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.8.1.7. However, the fields have a slightly different definition.

- SMB_FIND_FILE_BOTH_DIRECTORY_INFO\[SearchCount\]
- {
- ULONG NextEntryOffset;
- ULONG FileIndex;
- FILETIME CreationTime;
- FILETIME LastAccessTime;
- FILETIME LastWriteTime;
- FILETIME LastChangeTime;
- LARGE_INTEGER EndOfFile;
- LARGE_INTEGER AllocationSize;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- ULONG FileNameLength;
- ULONG EaSize;
- UCHAR ShortNameLength;
- UCHAR Reserved;
- WCHAR ShortName\[12\];
- SMB_STRING FileName;
- }

| 0                 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8        | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                    | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ----------------- | --- | --- | --- | --- | --- | --- | --- | -------- | --- | ---------- | --- | --- | --- | --- | --- | -------------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NextEntryOffset   |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileIndex         |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreationTime      |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastAccessTime    |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastWriteTime     |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastChangeTime    |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EndOfFile         |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AllocationSize    |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ExtFileAttributes |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileNameLength    |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EaSize            |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ShortNameLength   |     |     |     |     |     |     |     | Reserved |     |            |     |     |     |     |     | ShortName (24 bytes) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     | FileName (variable)  |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...               |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**FileIndex (4 bytes):** This field SHOULD[&lt;68&gt;](#Appendix_A_68) be set to zero when sent in a response and SHOULD be ignored when received by the client.

**CreationTime (8 bytes):** A FILETIME time stamp of when the previous version represented by the @GMT token was created. The FILETIME format is defined in [\[MS-DTYP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DTYP%5d.pdf#Section_cca2742956894a16b2b49325d93e4ba2), section 2.3.3.

**LastAccessTime (8 bytes):** A FILETIME time stamp of when the previous version represented by the @GMT token was last accessed.

**LastWriteTime (8 bytes):** A FILETIME time stamp of when the previous version represented by the @GMT token last had data written to it.

**LastChangeTime (8 bytes):** A FILETIME time stamp of when the previous version represented by the @GMT token was last changed.

**EndOfFile (8 bytes):** This field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**AllocationSize (8 bytes):** This field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**ExtFileAttributes (4 bytes):** Extended attributes for this file that MUST be marked as a DIRECTORY.

**FileNameLength (4 bytes):** Length, in bytes, of the **FileName** field.

**EaSize (4 bytes):** This field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**Reserved (1 byte):** An 8-bit unsigned integer used to maintain byte alignment. This field MUST be 0x00.

**ShortName (24 bytes):** The 8.3 name for the file formatted as @GMT~XXX where XXX is an array index value in decimal of an array of snapshots returned, starting at an array index value of zero. The **ShortName** field MUST be formatted as an array of 16-bit Unicode characters and MUST NOT be NULL terminated.

**FileName (variable):** An @GMT token that represents an available previous version for the file or directory.

##### SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO

The fields and encoding of the TRANS2_FIND_FIRST2 SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO Information Level response message are identical to the fields and encoding of the TRANS2_FIND_FIRST2 SMB_FIND_FILE_FULL_DIRECTORY_INFO Information Level response, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 2.2.6.2.2, with the addition of the **FileId** field described in the list that follows the table in this section.[&lt;69&gt;](#Appendix_A_69)

- SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO\[SearchCount\]
- {
- ULONG NextEntryOffset;
- ULONG FileIndex;
- FILETIME CreationTime;
- FILETIME LastAccessTime;
- FILETIME LastWriteTime;
- FILETIME LastAttrChangeTime;
- LARGE_INTEGER EndOfFile;
- LARGE_INTEGER AllocationSize;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- ULONG FileNameLength;
- ULONG EaSize;
- ULONG Reserved;
- LARGE_INTEGER FileID;
- SMB_STRING FileName;
- }

| 0                   | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NextEntryOffset     |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileIndex           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreationTime        |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastAccessTime      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastWriteTime       |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastAttrChangeTime  |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EndOfFile           |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AllocationSize      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ExtFileAttributes   |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileNameLength      |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EaSize              |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| Reserved            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileId              |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileName (variable) |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**FileIndex (4 bytes):** This field SHOULD[&lt;70&gt;](#Appendix_A_70) be set to zero when sent in a response and SHOULD be ignored when received by the client.

**EndOfFile (8 bytes):** This LARGE_INTEGER field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**AllocationSize (8 bytes):** This LARGE_INTEGER field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**ExtFileAttributes (4 bytes):** Extended attributes for this file that MUST be marked as a DIRECTORY.

**FileNameLength (4 bytes):** The length, in bytes, of the **FileName** field.

**EaSize (4 bytes):** This field SHOULD[&lt;71&gt;](#Appendix_A_71) be set to zero when sending a response and MUST be ignored when the client receives this message.

**Reserved (4 bytes):** This field SHOULD be set to 0x00000000 in the server response. The client MUST ignore this field.

**FileId (8 bytes):** A LARGE_INTEGER that serves as an internal file system identifier. This number MUST be unique for each file on a given volume. If a remote file system does not support unique FileId values, then the **FileId** field MUST be set to zero.

**FileName (variable):** This field contains the name of the file.

##### SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO

The fields and encoding of the TRANS2_FIND_FIRST2 SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO Information Level response message are identical to the fields and encoding of SMB_FIND_FILE_BOTH_DIRECTORY_INFO Information Level, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b), section 2.2.6.2.2, with the addition of the **Reserved2** and **FileId** fields described in the list that follows the table.[&lt;72&gt;](#Appendix_A_72)

- SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO\[SearchCount\]
- {
- ULONG NextEntryOffset;
- ULONG FileIndex;
- FILETIME CreationTime;
- FILETIME LastAccessTime;
- FILETIME LastWriteTime;
- FILETIME LastChangeTime;
- LARGE_INTEGER EndOfFile;
- LARGE_INTEGER AllocationSize;
- SMB_EXT_FILE_ATTR ExtFileAttributes;
- ULONG FileNameLength;
- ULONG EaSize;
- UCHAR ShortNameLength;
- UCHAR Reserved;
- WCHAR ShortName\[12\];
- USHORT Reserved2;
- LARGE_INTEGER FileID;
- SMB_STRING FileName;
- }

| 0                   | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8        | 9   | 1<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6                    | 7   | 8   | 9   | 2<br><br>0 | 1   | 2   | 3   | 4   | 5   | 6   | 7   | 8   | 9   | 3<br><br>0 | 1   |
| ------------------- | --- | --- | --- | --- | --- | --- | --- | -------- | --- | ---------- | --- | --- | --- | --- | --- | -------------------- | --- | --- | --- | ---------- | --- | --- | --- | --- | --- | --- | --- | --- | --- | ---------- | --- |
| NextEntryOffset     |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileIndex           |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| CreationTime        |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastAccessTime      |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastWriteTime       |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| LastChangeTime      |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EndOfFile           |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| AllocationSize      |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ExtFileAttributes   |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileNameLength      |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| EaSize              |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ShortNameLength     |     |     |     |     |     |     |     | Reserved |     |            |     |     |     |     |     | ShortName (24 bytes) |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     | Reserved2            |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileId              |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| FileName (variable) |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |
| ...                 |     |     |     |     |     |     |     |          |     |            |     |     |     |     |     |                      |     |     |     |            |     |     |     |     |     |     |     |     |     |            |     |

**FileIndex (4 bytes):** This field SHOULD[&lt;73&gt;](#Appendix_A_73) be set to zero when sent in a response and SHOULD be ignored when received by the client.

**AllocationSize (8 bytes):** This LARGE_INTEGER field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**ExtFileAttributes (4 bytes):** This field represents the extended attributes for this file that MUST be marked as a DIRECTORY.

**EaSize (4 bytes):** This field MUST be set to zero when sending a response and MUST be ignored when the client receives this message.

**Reserved (1 byte):** An 8-bit unsigned integer that is used to maintain 64-bit alignment. This member MUST be 0x00.

**Reserved2 (2 bytes):** A 16-bit unsigned integer that is used to maintain 64-bit alignment. This member MUST be 0x0000.

**FileId (8 bytes):** A LARGE_INTEGER that serves as an internal file system identifier. This number MUST be unique for each file on a given volume. If a remote file system does not support unique FileId values, then the **FileId** field MUST be set to zero.

#### QUERY_FS Information Level Extensions

##### SMB_QUERY_FS_ATTRIBUTE_INFO

For this Information Level, the server SHOULD check the **FileSystemAttributes** field and remove the attribute flags that are not supported by the underlying object store before sending the response to the client.[&lt;74&gt;](#Appendix_A_74)

#### QUERY Information Level Extensions

No new SMB-specific Information Levels are specified for these extensions.

#### SET Information level Extensions

No new SMB-specific Information Levels are specified for these extensions.

# Protocol Details

An SMB implementation MUST implement CIFS, as specified by section 3 of the [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) specification.

## Common Details

### Abstract Data Model

This section specifies a conceptual model of possible data organization that an implementation maintains in order to participate in this protocol. The described organization is provided to explain how the protocol behaves. This document does not mandate that implementations adhere to this model as long as their external behavior is consistent with what is described in this document.

The following elements extend the global abstract data model that is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.1.1.

#### Global

There are no global parameters defined as common to both client and server.

### Timers

There are no timers common to both client and server.

### Initialization

No new common variables are defined in this document.

### Higher-Layer Triggered Events

#### Sending Any Message

Processing of any message is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.1.4.1 with the following additions: The MD5 algorithm, as specified in [\[RFC1321\]](http://go.microsoft.com/fwlink/?LinkId=90275), MUST be used to generate a hash of the SMB message (from the start of the SMB_Header), which is defined as follows.

- IF ( Connection.SigningChallengeResponse != NULL ) THEN
- CALL MD5Init( md5context )
- CALL MD5Update( md5context, Connection.SigningSessionKey )
- CALL MD5Update( md5context,
- Connection.SigningChallengeResponse )
- CALL MD5Update( md5context, SMB message )
- CALL MD5Final( digest, md5context )
- ELSE
- CALL MD5Init( md5context )
- CALL MD5Update( md5context, Connection.SigningSessionKey )
- CALL MD5Update( md5context, SMB message )
- CALL MD5Final( digest, md5context )
- END IF
- SET the signature TO the first 8 bytes of the digest

The resulting 8-byte signature MUST be copied into the **SecuritySignature** field of the SMB header, after which the message can be transmitted.

### Message Processing Events and Sequencing Rules

#### Receiving Any Message

Processing of any message is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.1.5.1 with the following additions:

The MD5 algorithm, as specified in [\[RFC1321\]](http://go.microsoft.com/fwlink/?LinkId=90275), MUST be used to generate a hash of the SMB message (from the start of the SMB header), and SHOULD be used as follows.

- IF ( Connection.SigningChallengeResponse != NULL ) THEN
- CALL MD5Init( md5context )
- CALL MD5Update( md5context, Connection.SigningSessionKey )
- CALL MD5Update( md5context,
- Connection.SigningChallengeResponse )
- CALL MD5Update( md5context, SMB message )
- CALL MD5Final( digest, md5context )
- ELSE
- CALL MD5Init( md5context )
- CALL MD5Update( md5context, Connection.SigningSessionKey )
- CALL MD5Update( md5context, SMB message )
- CALL MD5Final( digest, md5context )
- END IF
- SET the signature TO the first 8 bytes of the digest

The resulting 8-byte signature is compared with the original value of the **SMB_Header.SecuritySignature** field. If the signature received with the message does not match the signature that is calculated, then the message MUST be discarded and no further processing on it is done. The receiver MAY also terminate the connection by disconnecting the underlying transport connection and cleaning up any state associated with the connection.

### Timer Events

There are no timers common to both client and server.

### Other Local Events

There are no local events common to both client and server.

## Client Details

### Abstract Data Model

This section specifies a conceptual model of possible data organization that an implementation maintains in order to participate in this protocol. The described organization is provided to explain how the protocol behaves. This document does not mandate that implementations adhere to this model as long as their external behavior is consistent with what is described in this document.

The following elements extend the client abstract data model specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.1.

#### Global

**Client.MessageSigningPolicy:** A state that determines whether or not this node signs messages. This parameter has four possible values:

- _Required_ -- Message signing is required. Any connection to a server that does not use signing MUST be disconnected.
- _Enabled_ -- Message signing is enabled. If the other node enables or requires signing, signing MUST be used.
- _Declined_ -- Message signing is disabled unless the other party requires it. If the other party requires message signing, it MUST be used. Otherwise, message signing MUST NOT be used.
- _Disabled_ -- Message signing is disabled. Message signing MUST NOT be used. The _Disabled_ state is obsolete and SHOULD NOT[&lt;75&gt;](#Appendix_A_75) be used.

**Client.SupportsExtendedSecurity:** A flag that indicates whether the client supports Generic Security Services (GSS), as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378), for selecting the authentication protocol.

**Client.Supports32BidPIDs:** A flag that indicates whether the client supports 32-bit process IDs.

#### Per SMB Connection

**Client.Connection.GSSNegotiateToken:** A byte array that contains the token received during an extended security negotiation and that is remembered for authentication.

**Client.Connection.ServerGUID:** A GUID generated by the server to uniquely identify this server.

#### Per SMB Session

**Client.Session.AuthenticationState:** A session can be in one of three states:

- InProgress -- A session setup (an extended SMB_COM_SESSION_SETUP_ANDX exchange as described in section [3.2.4.2.4.1](#Section_495dd941077648aaaa8af1aa5eeadcea)) is in progress for this session.
- Valid -- A session setup exchange has successfully completed; the session is valid and a UID for the session has been assigned by the server.
- Expired -- The Kerberos ticket for this session has expired and the session needs to be re-established.

**Client.Session.SessionKeyState:** The session key state. This can be either Unavailable or Available.

**Client.Session.UserCredentials:** An opaque implementation-specific entity that identifies the credentials that were used to authenticate to the server.

#### Per Tree Connect

**Client.TreeConnect.GuestMaximalShareAccessRights:** The **GuestMaximalShareAccessRights** value as returned in the [SMB_COM_TREE_CONNECT_ANDX server response (section 2.2.4.7.2)](#Section_087860d5391941d5a7531b330d651196).

**Client.TreeConnect.MaximalShareAccessRights:** The **MaximalShareAccessRights** value as returned in the SMB_COM_TREE_CONNECT_ANDX server response (section 2.2.4.7.2).

#### Per Unique Open

None.

### Timers

There are no new client timers other than those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.2.

### Initialization

Initialization of the following additional parameters is required beyond that specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.3.

The following values MUST be initialized at system startup:

- **Client.MessageSigningPolicy** and **Client.SupportsExtendedSecurity** MUST be set based on system policy.[&lt;76&gt;](#Appendix_A_76) The value of this is not constrained by the values of any other policies.
- **Client.Supports32BitPIDs** MUST be set to TRUE if the client supports 32-bit process IDs.[&lt;77&gt;](#Appendix_A_77)

When an SMB connection is established, the following values MUST be initialized:

- **Client.Connection.GSSNegotiateToken** is set to an empty array.
- **Client.Connection.ServerGUID** is set to the GUID of the server.

When an SMB session is established on an SMB connection, the following value MUST be initialized:

- **Client.Session.AuthenticationState** MUST be set to _InProgress_.
- **Client.Session.SessionKeyState** MUST be set to _Unavailable_.
- **Client.Session.UserCredentials** MUST be empty.
- **Client.SessionTimeoutValue** (see \[MS-CIFS\] (section 3.2.1.1)) SHOULD be set to 60 seconds.

All other values are initialized as specified in \[MS-CIFS\] section 3.2.3.

### Higher-Layer Triggered Events

#### Sending Any Message

The following global details are presented to a client that sends any message in addition to what is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.1.

##### Scanning a Path for a Previous Version Token

The application requests a previous version of a file by placing a time indicator in the path as a directory element, as specified in section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af). For any path-based operation (for example,SMB_COM_NT_CREATE_ANDX) the client SHOULD scan the file path being provided by the application for a formatted @GMT token.

If a previous version token is present in the pathname as a directory element or a final target, the client SHOULD[&lt;78&gt;](#Appendix_A_78) set the SMB_FLAGS2_REPARSE_PATH flag in the SMB header of the request.

#### Application Requests Connecting to a Share

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.

##### Connection Establishment

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.1.

##### Dialect Negotiation

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.2.

##### Capabilities Negotiation

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.3, with the addition that the new capabilities flags (specified in section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4)) are also to be considered in the list of possible capabilities.

##### User Authentication

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.4, with the following additions:

If **Client.Connection.ShareLevelAccessControl** is FALSE:

For each existing **Connection** to the server in **Client.ConnectionTable\[ServerName\]**, the client MUST search the **Client.Connection.SessionTable** for a **Session** that corresponds to the user that establishes the connection to the share. The client MUST search for a valid **Session** by either a [**security context**](#gt_88d49f20-6c95-4b64-a52c-c3eca2fe5709) or a **UID** representing the user.

- If a **Connection** with an existing **Session** for this user is found, then the client MUST reuse the **Session** and continue processing.
- If none of the existing **Connections** to the server has a valid **Session** for this user, the client SHOULD[&lt;79&gt;](#Appendix_A_79) reuse one of the existing **Connections** identified or established in section [3.2.4.2.1](#Section_40ac56691c634026bac4807c4981205a). The client MUST establish a new **Session** for the user. The user's credentials, typically a username or principal and an associated password or password hash, MUST be stored in **Client.Session.UserCredentials**.

Signing

The client-global **Client.MessageSigningPolicy** MUST be compared against the selected **Client.Connection.ServerSigningState**, as per the following table. If the result is _Blocked_, the underlying transport connection SHOULD be closed.[&lt;80&gt;](#Appendix_A_80)

| Client signing state | Server signing state | | | |
| --- | --- | | | | --- | --- | --- |
| | Disabled | Declined | Enabled | Required |
| Disabled | Message<br><br>Unsigned | Message Unsigned | Message<br><br>Unsigned | Blocked |
| Declined | Message Unsigned | Message Unsigned | Message Unsigned | Message Signed |
| Enabled | Message Unsigned | Message Unsigned | Message Signed | Message Signed |
| Required | Blocked | Message Signed | Message Signed | Message Signed |

If the client's **Client.MessageSigningPolicy** is "Required", the client MUST set the SMB_FLAGS2_SMB_SECURITY_SIGNATURE_REQUIRED bit in the **Flags2** field of the [SMB_COM_SESSION_SETUP_ANDX request](#Section_a00d03613544484596ab309b4bb7705d) SMB header to indicate that the client refuses to connect if signing is not used.

Extended Security

If **Client.Connection.ServerCapabilities** has the CAP_EXTENDED_SECURITY flag set (which indicates that the server supports extended security), then the client MUST construct an SMB_COM_SESSION_SETUP_ANDX request, as specified in section 2.2.4.6.1.

The client MUST do one of the following:

- Pass the **Client.Connection.GSSNegotiateToken** (if valid) to the configured GSS authentication mechanism to obtain a GSS output token for the authentication protocol exchange.[&lt;81&gt;](#Appendix_A_81)
- Choose to ignore the **Client.Connection.GSSNegotiateToken** received from the server, and give an empty input token to the configured GSS authentication protocol to obtain a GSS output token for the authentication protocol exchange.

In either case, the client MUST initiate the GSS authentication protocol via the GSS_Init_sec_context() interface, as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378), passing in the input **Client.Connection.GSSNegotiateToken** as previously described, along with the credentials from **Client.Session.UserCredentials**. The client MUST set the MutualAuth and Delegate options which are specified in [\[MS-KILE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9) section 3.2.1.[&lt;82&gt;](#Appendix_A_82)

The client MUST then create an SMB_COM_SESSION_SETUP_ANDX request (section 2.2.4.6.1) message. The client MUST set CAP_EXTENDED_SECURITY in the **Capabilities** field, SMB_FLAGS2_EXTENDED_SECURITY in the **SMB_Header.Flags2** field, the **SMB_Data.Bytes.SecurityBlob** field to the GSS output token generated by the GSS_Init_sec_context() interface, and the **SMB_Parameters.Words.SecurityBlobLength** to the length of the GSS output token.

###### Sequence Diagram

For the user to be successfully authenticated and to establish a session, the client MUST follow a security negotiation scheme that can involve one or more roundtrips of SMB_COM_SESSION_SETUP_ANDX request and response. In each roundtrip, the server and client exchange security tokens. The exchange of security tokens MUST continue until either the client or the server determines that authentication has failed or both sides decide that authentication is complete. If authentication fails, then the client drops the connection and indicates the error (see the following diagram for details). If authentication succeeds, then the application protocol can be assured of the identity of the participants as far as the supporting authentication protocol can accomplish.

In the sequence diagram that follows, requests with straight line arrows stand for the requests that the client MUST send. Requests with dotted line arrows stand for the requests the client could send. The server MUST respond to each client request that it receives.

![User authentication and session establishment sequence](data:image/png;base64,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)

Figure 2: User authentication and session establishment sequence

The diagram illustrates the sequence of events during the protocol negotiation and session establishment process. After the initial SMB_COM_NEGOTIATE command exchange has been completed, the SMB_COM_NEGOTIATE exchange MUST NOT be repeated over the same SMB connection; otherwise, the server disconnects the client by closing the underlying transport connection. The parameters returned in the SMB_COM_NEGOTIATE response MUST be used when creating new sessions over the same connection.

**Session Setup Roundtrip**

The SMB_COM_NEGOTIATE Server response is processed as described in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.2. The protocol extensions in this document apply only to the NT LM 0.12 dialect of SMB. For further information on SMB dialects, see \[MS-CIFS\] section 1.7.

If the NT LM 0.12 dialect is successfully negotiated, then the SMB client examines the Capabilities field in the SMB_COM_NEGOTIATE server response (section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4)). If the CAP_EXTENDED_SECURITY bit is clear (0x00000000), then the SMB server does not support extended security. In order for authentication to proceed, the SMB client MUST build a non-extended SMB_COM_SESSION_SETUP_ANDX request, and MUST set the **WordCount** field to 0x0d. Authentication then proceeds as described in \[MS-CIFS\] section 3.2.4.2.4.

If the CAP_EXTENDED_SECURITY bit is set (0x80000000), then the SMB server does support extended security. The SMB client MUST build an SMB_COM_SESSION_SETUP_ANDX request in the extended form, as specified in section [2.2.4.6.1](#Section_a00d03613544484596ab309b4bb7705d). The request is sent to the SMB server, and the server builds an extended SMB_COM_SESSION_SETUP_ANDX server response (section [2.2.4.6.2](#Section_e5a467bccd364afa825e3f2a7bfd6189)). The security BLOB in the session setup response is built as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378).

Upon receiving the extended SMB_COM_SESSION_SETUP_ANDX server response (section 2.2.4.6.2), the SMB client invokes the local security package to determine whether the session setup request SHOULD be completed, aborted, or continued. A completed session indicates that the server has enough information to establish the session. An aborted session indicates that the server cannot proceed with the session setup because of an error in the information presented by the client, or otherwise. If the session setup has to be continued, the security package on the client and/or server requires an additional roundtrip before the session setup can be established. This is especially true of new security packages that support mutual authentication between the client and server.

In the case of extended security, the SMB protocol does not make the distinction between NTLM and Kerberos; therefore, the sequence defined previously in this section is the same in both cases. If authentication succeeds after a single roundtrip, then only one session setup exchange is required. Otherwise, additional roundtrips will be required.

Each additional roundtrip MUST consist of one SMB_COM_SESSION_SETUP_ANDX client request and one SMB_COM_SESSION_SETUP_ANDX server response. In the sequence diagram, this is represented in the horizontal dotted line that symbolizes additional roundtrips until the final roundtrip, which is represented as SMB_COM_SESSION_SETUP_ANDX Client Request N and SMB_COM_SESSION_SETUP_ANDX Server Response N, where N is a number larger than 1.

All additional SMB session setup roundtrips follow the same sequence details as Session Setup Roundtrip, as described earlier in this topic.[&lt;83&gt;](#Appendix_A_83)

##### Connecting to the Share (Tree Connect)

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.2.5, with the following additions:

If a tree connect is already established to the target share in **Client.Connection.TreeConnectTable**, then it SHOULD be reused. If not, then the client creates an SMB_COM_TREE_CONNECT_ANDX request, as specified in section [2.2.4.7.1](#Section_16b173568eff49c29d21557e07ef085d), to connect to the target share.

**Session Key Protection**

The client SHOULD[&lt;84&gt;](#Appendix_A_84) request **Client.Session.SessionKey** protection by setting the TREE_CONNECT_ANDX_EXTENDED_SIGNATURES flag in the **Flags** field of the SMB_COM_TREE_CONNECT_ANDX request to TRUE.

**Extended Information Response**

The client MUST request extended information in the response by setting the TREE_CONNECT_ANDX_EXTENDED_RESPONSE flag in the **Flags** field of the SMB_COM_TREE_CONNECT_ANDX request to TRUE, as defined in section [2.2.4.3.2](#Section_056d7d3304574f9ab7e0ab983ce24ae4).

The client sends this message to the server.

#### Application Requests Opening a File

The processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) sections 3.2.4.5 and 3.2.4.6, with the following additions:

The application can request that additional information be returned for the Open, in particular maximal access information. The client can issue either an SMB_COM_NT_CREATE_ANDX request or an SMB_COM_OPEN_ANDX request to make use of these extensions, as specified in section [3.2.4.3.1](#Section_28ca564a5aa34ef3a245829627a7b37e).[&lt;85&gt;](#Appendix_A_85)

##### SMB_COM_NT_CREATE_ANDX Request

To access these extensions, the application can also provide:

- **RequestExtendedResponse:** A BOOLEAN. If TRUE, then it indicates that the application is requesting a server to send an extended response.

If the application is requesting an extended server response, then the client MUST set the NT_CREATE_REQUEST_EXTENDED_RESPONSE flag in the **SMB_Parameters.Flags** field of the request.

For a named pipe request, the client MUST set the SYNCHRONIZE bit in the **DesiredAccess** field if the FILE_SYNCHRONOUS_IO_ALERT or FILE_SYNCHRONOUS_IO_NONALERT bit is set in the **CreateOptions** field.

##### SMB_COM_OPEN_ANDX Request (deprecated)

To access these extensions, the application can also provide:

- **RequestExtendedResponse:** A BOOLEAN. If TRUE, then it indicates that the application is requesting a server to send an extended response.

If the application is requesting an extended server response, then the client SHOULD[&lt;86&gt;](#Appendix_A_86) set the SMB_OPEN_EXTENDED_RESPONSE flag in the **SMB_Parameters.Flags** field of the request.

#### Application Requests Reading from a File, Named Pipe, or Device

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.14, with the following additions:

The SMB_COM_READ_ANDX command request has been extended as specified below.

**Timeout_or_MaxCountHigh**

The **Timeout** field specified in \[MS-CIFS\] section 2.2.4.42.1 has been extended to be the **Timeout_or_MaxCountHigh** field. This field is treated as a union of a 32-bit **Timeout** field and a 16-bit unsigned integer named **MaxCountHigh**, as specified in section [2.2.4.2.1](#Section_df9244e87b2d4714a3836990589a8ff4).

- For pipe reads, the client MUST use **Timeout_or_MaxCountHigh** as the **Timeout** field. The client MUST set the **Timeout** field either to a time-out value in milliseconds, or to 0xFFFFFFFF.[&lt;87&gt;](#Appendix_A_87) The latter value indicates to the server that the operation MUST NOT time out.
- For file reads, the client MUST use this as the **MaxCountHigh** field. If the count of bytes to read is larger than 0xFFFF bytes in length, then the client MUST use the **MaxCountHigh** field to hold the two most significant bytes of the count, thus allowing for a 32-bit read count when combined with **MaxCountOfBytesToReturn** field. If the read count is not larger than 0xFFFF, then the client MUST set **MaxCountHigh** to zero.

##### Large Read Support

If the CAP_LARGE_READX bit is set in **Client.Connection.ServerCapabilities**, then the client is allowed to issue a read of a size larger than **Client.Connection.MaxBufferSize** using an SMB_COM_READ_ANDX request. Otherwise, the client MUST split the read into multiple requests in order to retrieve the entire amount of data.[&lt;88&gt;](#Appendix_A_88)

- If a large read is being issued and the object being read is not a file, then the count of bytes to read MUST be placed into the **MaxCountOfBytesToReturn** field. This field is a 16-bit unsigned integer; therefore, the maximum count of bytes that can be read is 0xFFFF bytes (64K - 1 byte).
- If a large read is being issued and the object being read is a file, then the two least significant bytes of the count of bytes to read MUST be placed into the **MaxCountOfBytesToReturn** field and the two most significant bytes of the count MUST be placed into the **MaxCountHigh** field.

#### Application Requests Writing to a File, Named Pipe, or Device

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.15 with the following additions:

The SMB_COM_WRITE_ANDX command request has been extended as specified in section [2.2.4.3.1](#Section_178be656705649ea8bcbcf123737b016).

Large Write Support

If the CAP_LARGE_WRITEX bit is set in **Client.Connection.ServerCapabilities**, then the client is allowed to issue a write of a size larger than **Client.Connection.MaxBufferSize** using an SMB_COM_WRITE_ANDX request. Otherwise, it MUST split the write into multiple requests to write the entire amount of data.

If the CAP_LARGE_WRITEX bit is set in **Client.Connection.ServerCapabilities**, and the client is issuing a write of a size larger than **Client.Connection.MaxBufferSize**, the client MUST ensure that the total length of the SMB packet does not exceed the maximum packet length allowed by the underlying transport, as specified in section [2.1](#Section_f906c680330c43ae9a71f854e24aeee6).

If the count of bytes to be written is greater than or equal to 0x00010000 (64K), then the client MUST set the two least significant bytes of the count in the **DataLength** field of the request and the two most significant bytes of the count in the **DataLengthHigh** field.

#### Application Requests a Directory Enumeration

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.27, with the following additions:

The TRANS2_FIND_FIRST2 subcommand request has been extended as specified below.

New Information Levels

To request the new Information Levels specified in section [2.2.6.1.1](#Section_7875b17bfe7b4810be2bc314272bce9c), the client MUST set the **InformationLevel** field of the TRANS2_FIND_FIRST2 request to the corresponding Information Level.

Enumerating Previous Versions

An application is allowed to request an enumeration of available previous versions of a file or directory using a TRANS2_FIND_FIRST2 request (see section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af)). To do this, the request MUST have the @GMT token wildcard, @GMT-\*, as part of the search pattern in the **FileName** field and it MUST use the SMB_FIND_FILE_BOTH_DIRECTORY_INFO Information Level. This extension is not available for Information Levels other than SMB_FIND_FILE_BOTH_DIRECTORY_INFO. The client MAY[&lt;89&gt;](#Appendix_A_89) fail such requests or simply send the requests to the server. Because it is a path-based operation, this request follows the same previous version token parsing rules as specified in section [3.2.4.1.1](#Section_ad27e016fb4c410590985f85ac6c8c7e).

The message is sent to the server.

#### Application Requests Querying File Attributes

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.12, with the following additions:

**Pass-through Information Levels**

The extension adds support for pass-through Information Levels, as defined in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475). If the CAP_INFOLEVEL_PASSTHRU bit in **Client.Connection.ServerCapabilities** is set, the client MUST increment the Information Level value by SMB_INFO_PASSTHROUGH (0x03e8) and place the resulting value in the **InformationLevel** field of a TRANS2_QUERY_FILE_INFORMATION or TRANS2_QUERY_PATH_INFORMATION request.

**File Streams**

A client can send a TRANS2_QUERY_FILE_INFORMATION subcommand of the [SMB_COM_TRANSACTION2 request](#Section_714bb6fa7fab4dab8ff88a01c273b9ce) to the server with the **InformationLevel** field set to SMB_QUERY_FILE_STREAM_INFO (see \[MS-CIFS\] section 2.2.6.8). If the FID field in the client request is on an SMB share that does not support streams, then the server MUST fail the request with STATUS_INVALID_PARAMETER.

A client can send a TRANS2_QUERY_PATH_INFORMATION subcommand of the SMB_COM_TRANSACTION2 request to the server with the **InformationLevel** field set to SMB_QUERY_FILE_STREAM_INFO (see \[MS-CIFS\] section 2.2.6.6.2). If the **FileName** field in the client request is on an SMB share that does not support streams, then the server MUST fail the request with STATUS_INVALID_PARAMETER.

Previous Version Tokens

Because the TRANS2_QUERY_PATH_INFORMATION subcommand request is a path-based operation, the path SHOULD be scanned for previous version tokens by the client, as specified in section [3.2.4.1.1](#Section_ad27e016fb4c410590985f85ac6c8c7e).

#### Application Requests Setting File Attributes

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.13, with the following additions:

**Pass-Through Information Levels**

The extension adds support for pass-through **Information Levels**, as defined in section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475). If the CAP_INFOLEVEL_PASSTHRU bit in **Client.Connection.ServerCapabilities** is set the client MUST increment the level value by SMB_INFO_PASSTHROUGH (0x03e8) and place the resulting value in the **InformationLevel** field of a TRANS2_SET_FILE_INFORMATION or TRANS2_SET_PATH_INFORMATION request. The serialized native structure is placed in the Trans2_Data block of the request and the **SMB_Parameters.TotalDataCount** is set to the length of this buffer.

**Previous Version Tokens**

Because the TRANS2_SET_PATH_INFORMATION subcommand request is a path-based operation, the path SHOULD be scanned for previous version tokens by the client, as specified in section [3.2.4.1.1](#Section_ad27e016fb4c410590985f85ac6c8c7e).

#### Application Requests Querying File System Attributes

The processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475), with the following additions:

Support of pass-through Information Levels for the querying file system attributes through the use of the TRANS2_QUERY_FS_INFORMATION subcommand is defined in section 2.2.2.3.5. If the CAP_INFOLEVEL_PASSTHRU bit in **Client.Connection.ServerCapabilities** is set, the client MUST increment the Information Level value by SMB_INFO_PASSTHROUGH (0x03e8) and place the resulting value in the **InformationLevel** field of a TRANS2_QUERY_FS_INFORMATION request.

#### Application Requests Setting File System Attributes

The application MUST provide the following:

- An **Open** that identifies a file on the file system that will have its attributes set.
- The Information Level that defines the format of the data to set. Valid Information Levels are specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.5.
- A buffer that contains the attribute data to be set on the server. The buffer is formatted as specified in the subsection of \[MS-FSCC\] section 2.5 that corresponds to the Information Level supplied by application.

This operation supports the use of pass-through Information Levels only. If the CAP_INFOLEVEL_PASSTHRU flag in **Client.Connection.ServerCapabilities** is not set, then the client MUST fail the request and return the error code STATUS_NOT_SUPPORTED to the calling application.

The client MUST construct a TRANS2_SET_FS_INFORMATION subcommand request, as specified in section [2.2.6.4.1](#Section_cf5f012fe1c4499d9df8a95add99221d).

The client MUST use **Open.TreeConnect** and **Open.Session** to send the request to the server. The request MUST be sent to the server, as specified in section [3.2.4.1](#Section_27c46f8bf18145c89e4b0383493bc009).

#### Application Requests Sending an I/O Control to a File or Device

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.22, with the following additions:

A list of FSCTLs is specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) section 2.3. Three I/O control codes specific to the extension are described in the following subsections.

##### Application Requests Enumerating Available Previous Versions

An application can request that a client retrieve an enumeration of available previous version time stamps for a share by issuing the FSCTL_SRV_ENUMERATE_SNAPSHOTS control code, as specified in section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17).[&lt;90&gt;](#Appendix_A_90) The request is sent to the server.

##### Performing a Server-Side Data Copy

An outline of the steps taken for a server-side data copy follows. The application first requests the FSCTL_SRV_REQUEST_RESUME_KEY operation and the client issues the request to the server as specified in section [3.2.4.11.2.1](#Section_544b0e52d2a9475c8bdfc5d2ac8c877d). The client then returns the Copychunk Resume Key received from the server to the application. The application then requests the FSCTL_SRV_COPYCHUNK operation and the client issues the request to the server as specified in section [3.2.4.11.2.2](#Section_2441bfb1323f49c1ac221260c4cd8958). The client then returns the status received from the server to the application.

![Server-side data copy of an entire file](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAkUAAAJECAYAAAAYIlBkAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAAEBcAABAYAQ1Vg2AAAH0ASURBVHhe7b0rtCzFtm69xBZLHLHkEkcgEIgrkAgEEnEEFrEF4gokAoFAIo5AIhEIzBEIBGILJBJxBQKBRCIQ2Pn/vfb81hlrEFmV9cysnL23Fq0yIuOVkREjvox81LOHFfDTTz89/Pjjjw/ffffdw+eff667oaPNafuff/758WyIiIg8TW4uihBAn3zyycMbb7zx8OzZs5175513Ht57772HDz74YDhx667naHPa/u233351Pt56662Hzz77TKEkIiJPipuIIlYjPv7444eXL1/uBNCXX3758Ntvvz3ulbXxyy+/PHzxxRc7oYR45dz98MMPj3tFRES2ydVE0R9//LFbbXjx4sVuNeKrr756+P333x/3yr2AeOXcvf/++7tzyeoS51ZERGRrXFwU/fXXX7tVBlYY+HUC3Q6cS0QRt9dY7dsqOU6dbqtuKbuM3eB2vU63NsciDlxUFH399dc7MUTmiqHtwopfngvj1ujWYNJgkPSJRKfbgkvfXgLKRhjxcodOtxb37bff7uYzuIgo4oFcnj/56KOPvEX2hODWGrdGOfc8h7QVMnmIbJEl+zeiiElIZE0wl11MFKGweHjaN5WeLpx7+sBWHsZWFMmWURSJvM7FRBED68MPP9w9RyRPG26X8jD2Fp41UhTJllEUibzO2aIIEZRvColUeNbo3oWyoki2jKJI5HXOEkUk5lbJFh+wlcvALVWM370+bK8oki2jKBJ5nZNFEZMcD9X6/JAcgi+X36swUhTJllEUibzOSaKI2yE8M2KHlrnw4DV95t5QFMmWURSJvM5Jooi/euDLxiLHwAc881Gse0FRJFtGUSTyOkeLIt4o4gFakVPgwWueM7oXFEWyZRRFIq9zlCiiA3MLxNfu5VToOxhDnjO6BxRFsmUURSKvM1sU8XVq/ufKv+yQc6Ev8ZD+PXzxXFEkW0ZRJPI6s0URt8y2/Mefclvu5Tasoki2jKJI5HVmiaL8n9k58IFHHrQ9F/JIPjzsfeoD3wzGOiDPyWsJfv31193/y12C3ha1ja8Ft9HoeGtfLZqaNGifd9999+HNN9/c/X7zzTe7cPp5PlPB+eE8nQr98c8//3z0/Z1z87801IW2qI72gO+//37nT3tR93pstF/dT/ty/GyPHGWR96Hjp4xTx3Vv/1uMi1ujKBJ5nVmiCONzzgcaMXjkgcE7l2qYMFj7Jo19dAN3Tl5LcakJsbdF91+Lr7/++mLC7lqMJg0mZeodQYdhz+TPvoiic89PzWvEpc7/paCujHF+q6N9CM8ESLvRv1L/Tz/99JXQAfbTvjUP9hMvfkhZ+0jaU+jtf6txcUsURSKvc1AUYRTOXSXCuND58xswfBgZwjFw1eAwycRY4rjShGqYuJKrV4HJC0daIJ+EZeLC+FIejjBEW8+L9OwnXVYBgPjEI5z9dV+n5sGxxujXbah+2psyUreahjbgN+Fsh3qlXcNrm9TwQD6kSXnkQ5qUk/BKwskz5+VU1v4B0D5ppJ2nYF+Ohzas53nUbvQl2ru3NeUkLmE1n1DznzrPbCecOAmr+VX/VP+jvjV8dAFBWvZ3UrcRCCDSHFoxrPUPpDvUd2qd0y6xRQkfMWp/yiePmmcl4aSZGhdT7Tg1ftkmTcrlt9pQ6pX4U+cuti3nIeFwKVHERPLy5cvd51rmCh1FkayRg6Lo3EkLY8dABAZmHfCE4ydOjGMGCdvEB8LwAwM7xrFv17wTXgcdIiViqaaF6idO8sJoUc9RvTBWqVeHvPqxph3rNkztiwEFwnHUg/04/EAY2zGw1J/yaxzA8I6oxw5sky7GkzrkmNlOPvXcngorkDnGNdInjd5WHdpjdC6n2m1fW9f0I7J/6jyTd/o7ZKLu+VZ/3a79j36e/kV/2yeK0kY44nFshFMX0tZJudd9CuKQX4U863GM6PnHziQdbT3V/2pbAOUTds64GLXj1PgFwnHEoS6UUe0K26P+Us8deVEuaevxwCVF0bNnz165OQJJUSRrZK8ousRXiBmQMcwMegZnqIMYqjGo8SBx2Z84dbsaugoGijgYjxgYqGmh51UNd99X6f7Q61OPtR93/BgttlMeLvn3NGwTBhwbLmloR/ZlMiJf2mGKpAtT/kwo8eP6cZ7CmleLLiGK9rVbz6/6+znvZP/UeWayJE6feHq+8e/rf6l/HRcd8ujHmYmeeqVfEodfwkjD9iFSrwr51OMY0fPnGLsIyjF2SFfzzzGF+I8ZF4lX23Fq/EKvA9T6Jt6+cxf/iGuJouqmBJKiSNbIXlHEQOW5j3NgYHbHAIZ9Rod4lcStcfbFhzpZkJYBGCNS08K+vPbtG5ULhNdjq8faj7seG23OdnU1TmA7x8IvE2BNE6PLFSN5EifxO/X4YMqfMms5uHNZ85toI1FEe06R9qnbuKl2m2prqHmNqPunznNWQuiPqXfPN/4cG9vVAf2pipqROCLu1HjoUA75zU1DubWdoI+xEeyv7UEelFuZKr+3Uz03EH/K4Le6EaN2ZHtq/PY6AG1HfOxazin1mDp3vd4VvjD/z3/+c2cbz3F8kHUkiLqrAmkkijhujo3wUR+T28DFDOfhKbJXFL148eKs7xJlwFfqlVof8IRnkFRDVVeY6gCv26SN2Arsi9EAJo7Up6aF6icOcUOMEHQDus+g1vrUY+3HHT/ttS+/moZtwoC61eME2ixX6aHnEfa1BVQ/9aurEZegdsK10UVRVgV6O8Zf27huT7XbvraeOl8h+6fOcw2vY6jnG/++/ldhUq91DuQxSj+a3Mgj4oTyuwGmrWr9idPLHJ2HDvtJG7o/53NE2iXUcwPVTx7Hjou049T4hV4HiF3F5qVt9527Xu8KAoWxh0A5x73zzjtDETRyzCscLyvEVRQRxnFRV9qG45mqt1yW3kf6ONkCHFO0xz4mRVGU/DlUMVGJAaHRMwiobK0wcfCzL3Ggnry6HUMRY5sy+MWfAYcDji+DDgFU86r7ki4QXun+QJ4pm/Rsx7iRb+ra940MAxBWjWPvtGyTNnVO2+U3ZY3Y1xZQ/Qi9xMX1ep3KWm+hdVEEaYO0N22Qtq3tUben2i3+UP2Jx+9IWNQ8RueZsNoncJB85/Y/9iUsaUf1IR7xO9iA2l7UizxC0tU+i7+WkfIrxCG8ulG9iMexJH1tF9L0i6mQ/fySL7/JA6p/zrjY145sp078UseE93yAcFxldO4g5Y0Y9e9T2Hf7DBchVN9iZn6JKKIdUt9KFcbEoS8lTUgb0k7Ejz/gTz7MCZyrfs7TxsTt6SFhlN3ntOxL+UA5xBv1LeIRXkX0VL0S3o+ZPMi/15VwzvUofBQ/0F9wOQZ+6Us5jl7+VH0r5JP2IL9AHabyTDjbodf51GOmrhlPPW5lUhRxBUGG5zBVcMKpIJMwjVAbDRgghLG/5lMbum4H8qqNTePWBqt5sU0ZyafmlRPUT3qvZ/dXyIP9/OZkBOpI/uyrdQL87KvxexzoYcQfpSOstskI4pFu1BajtslAZfsSXMo4X5p99co5nDpP/fyM2u1QW5N3zb/SyxqdZ9LS/3seqfvc/pe64/ad86m6koZ9Pd8K4aO6AnWq7QLE625Eyq7HyfGPDGmn5nvoXKWN6vntJM6oHSlnX3+qkHZUxujc9XpWLjXuRqJoJIQqI1FU612hvSL4qmgkPunYh+PYu7ginLaijOSBAGA7kIY8CSP/DuHZh0sZo/I5JsJG5SQP9rFN+ql6MffV8CqU8RNOWNosdUs4vzAVP9A3CE8a6p/jIoy0OR7Y144V9rMvjnRT5zHnn/xSXupJ/Frn6j/mmOtxcJ6mmBRFBLLzmvSDrVD5LbHvWOXh4Zdfftn9jczauNSkIbJGLi2KDgmhShVFwGSF3cdhL+vEhb8KO/xMpNhU4td9TIJMvkD+mXj5reUxSaYM8thnnykPkRLIi7Sj8ntc2oO4OLY7U/WiPdju9PKAtshxAvuJB6P4HcrCBY6L4wjUL/597Vghv3685NHbKnXPOQPCcz7qNsR/7DH3Y5piKIr4s07uEV8bGnLqZNUTtAX2Hav8m1sI8WNRFMmWuVT/5tnTYz/w20VRYPLKigKTKn4mOSbAOPZFFPWJjjwzWfKbMkZ5ZDLPZDoFcSkrRESMyu95JS7x6sQfpurF8bEPP8IDP9AmSZP6k3/PBwej+J3UMfTjqv6p+nZGeY7S5jhr+xIef92G+I89ZtKQ9hBDUUQm3D4TuSV05HPfdrw0iiLZMkv27ylRFJjEmNi49cXviKmJjjAm27qPPCIsOlP5h0zEIasjo/J7OREH2LeReNhXL2BfBECFtqNs8kRsRRBMUeN3UsfQj6v6D9U39Dz3nUfCa/tSVvx1G+I/9pjrMexjKIqOHShzGujS1Ea6BEscw1MiA3v0HETg9eA6iNbAkpOGrIet2ocl+3cVRdhzREPaGTuBP7ePmPzqrSQmROJOTXTYEcKrPWGbfLJiT9m5zTU1WQfyyioP6YhPHUflU0/qDsRhP2kyQccG5him6kV44lJOxEjqAVVosb/etiP9vvgVwlJn6tGPq/r3tWOFeLjK1Hmk7JSf9qVMIE3qTPy675hjJpz41Dt1Jy35V4aiiEwOXbFTAJnRUDgKy0HdAsq7JBxDGnofNDplx9EGtMU9QH3rMdJxeoe4BpRD+9J2dPgp2I8wWhNrFEUxInI75tqHe2MtogiYLDOXxF5U6POE49hmYpsaC4gJbFtERSDP5MH+lH/IDiZ+0ibdVPkcS46jTtCZhAnPMcCoXkzkbGeeof8Rv7ZDFRiZkxOfffvid0iDi4Cox9X9U+1YIZ+ImUqtD9tpA7bJK/sz3igbP8dF/dlPWPaRZu4xs50ygN/eJkNRxFes+Zr1PqhEPdnQO/E+I3KugaF8oNP3jj+HXn49CftIZwhp5E5OWmcqfB+9XlN5HKo/bZY4ESqdU9szkLbXj3JGg6Zziz+IPeZBUFijKOIcTp27uf3rlH54bt+Yy1Q/PqXOPc1U3ofGziH7cKu2uTRrEkVr5tD5l8tyq/amnD5uh6Lo0DdjomCnqCqyHlwPx1EhDBfbFfxJR1k1DTDBRw2yHbVHmlq36qf8mhfp0iD4U96+FZQuikhT/alTXPKnfjWcYz6mrriUTb35DRE4caOlTIgoYn9NH2oeqQe/Vfzua5t+7ED81HcqXcBAYiivCW/IxM0RSGsURbQjbcov9YdR24+o56Oek319EWreNQ1+yiZP+ie/1ciwj/AOYewjL9KQ11Q/Ho0dIF0l/tQrjnBsVvWnjlNldthHvtDHQE2fcH6JF3qataAomgfnLudfrs8t2jsrSp2hKCKAHVNg0EaGDjBY1VjR6TEWEEMYMHbxEycDpKbpRjUNRRnZxsB1gxiqv5df8yYOcXv6DvGzn3LZTh4Yvpp/9dc2CcfUlfC6FJnjTx1C91dIQx78ZlIIlFXzx0/967kAOuvIkJG21rfWn/TU9RC3eC2/iqLqpgTSGkVR7zf72r6ScZLl6ppPz7P6yWvUN4hDfrU/MKZzgQLsH5FxlH64rx9P5dHD40+9cpzVzgDblL+vzA7h5Nvbaapt5o6bpVEUibzOUBQxSewDg4Ib0Q00xFj1dNXAYEhyJRXDAqTtS+CwzyBWY1T9vfzqJw51r2lHEJ+yqCu/dQIgjPT81m3INmXkeI6pK+HV+OInPmGpT1xvm0A46fhN+4aeB/Fq21DnfZMGaWqe9Vj4xX8IXuv9j//4j52hvJarQmjKVYF0D6JoX9tXev+u8Xqa6p/qG6Ny6CPpf9Sp24LQ+/e+fkwZ+OvYgewP8fd6UQ7pQ8o+ZuyQX28/6OnZn+Ni+9C4WRpFkcjrnCSKMA4YgBHsO0UUAfEYJNUwsY1R6dQ4EP8+497Lr37ixHXBUCF+8mOpvdaPNtmXlmNDRJGGtMfUlfDuJ/6ovaegXNLg2K51rcfRSRk4tkf0Y6/Hkroe4q+//tr1PYzltRz5z3HPnz9/+PDDD3dOUTTuG1PlpD7sG13QQO/fh/pxHzvAdiX+Xi/K6X7cMWOH9HGXHDdLoyiax1Q/lr8zpQ3uhaEo4kr50B/BYgyqcQAMDZ2nGis6fQxSjFHoxiJGr8epfgwe1DIg/m4QyX+q/OonDmkxcKNjC8Sfyp80vUNw/CzjV8PJcZLPMXUlvPtrfWv+aaMO8bKP3+qnneuqF/SrctwU1J08AnWNP3U9RO2M16IKn+4ihPjHbwQaLDlpTEFb9n4z1faVjM3cVopwgVGe8U/1jZ4m0OcpZ59x7P17qh9PjR2ox5IyodeL+N2PO2bskJ59SRP7MNU2gbip1xpRFB1mqp/LmDX39zkMRRHPdfB8xz7SUeJoiBjiGKGEx9AQjj/7uuEeGSnAuCYNDnrDVz9xUg5lxDjHGIbqJ07qyS/pq3ELxO/GvpaBgax1ZR/51DBcYHtOXQnv/tQXA508ev4V4iQNJF2OkzxrHkyMgbrh9sH+mj7Uuu7jFl9SnyOEKmsURZA2zjmZavsO/TN9Jec7sD3qizDqG5zTGqdCPhEOI3r/hlE/3jd2+rGwDb1elNP9KXvu2CF9+jC/pLnUuFmSpyyK6AOIas5XzhmrkDlnqVv6GfETL/0nxJ/8mMMIIw/89BXyoa/Uh/nZTzjlTY2X5Mf+Wi7b5Jc6Qcqt4alTqH7KTx1TN/aP6gqjMjsZh8Bx17ij9mV/nfNzDEsxFEXHdtYYh06fCDnQfQfLSaexLsFUnW7FqHxOdj354VJ1vVQ+ozpi7EfhHeKcWg+e4WHAXZM5QqiyVlE04ti2Z3x2EXAo/dw+0PM9hlEdyHNU9lT4sVxi7IzqMXfcLMVTFkURw8xJTMxM2PgZF9Qr5455KeHpJ3Xih/gjmLFj5Jt8cOSDYz/5khfbpMFNzX1Jj1DJ/ImfOpMuogrIj+MgnLiUwTbxQ/UThzQ5frbZ1+sKU2V2SAOkIw3xYap9qUPNqx7nEgxFERW8xt8tcKD7DpZGWnKQyBgGx7XFCjDgrv33MnOEUOWeRNGxdGN5KbAfSxq1tXCrcXMOT10UsSoSGAsRFLj049E4ycQf4h/FxU94iB9XRccUqVegzehXySP5AL+9Tdlf61T9fU7uZaWu+8rsEE67Jn5I3kmf9uX4a15sH2qTazIURdf6uwUaqnbCTm1AWQ+HztulWKMAWWOdLsklVkg618jzHrnVuDmHJfv3GkRRBX9EQhz1Y16qogJGaWEUN8IiVD+rIvhxbI/o6akXgqPXFRDixKc+EeS9TtVf0wLh3U98wqbK7FA26VJ+mGpfQCBldYrtJRmKIiq7tr9bkO3DYPAPYUVuh6Lof2EiH9WniwroaeMfxY2wCN0PWS3p4dDjU8dexoipla7qjzgJhHc/8eeWCTkOyq8Ch/RT5zv574tzK4ai6BbPdlwDGnPpBr02WZKn49Yr8tqR75WljeSIrYsi+hC3LSucA/pTNcQj2E9a+uRcMP5zVk/2rTYdqhccW7deHnXcwpg6hKLof+HWDmG5xcP5x0WwEJ76MnmzDz/2OHmRrosH/LXPxp/82U7Zo9tGPT0QxsoS4akD24gQfnHEId/UPysxhOMgdQiEd3/KZruXOSJtAUkDU+0biItbmqEo4pkLXss/5tmLJUhHCL2RtwYdMZ0UI84AiDGvHfEe4RMQ9Lm1sXVRRH+KoQf6Ua4w2Vev9CrZzy9xSDcy6BX6LfH2jVHESIxnHdsQAXeorx9Tt33lkX7qlsZWOKZ/c7FMe15qnC4tikb9kPpwzjlO+loEPH2E+LmAiP3F0W+SF/ETJ9R8IP7EZR6jzC7MQ08fkpay046IntSLOge2azmkBdLVc0B49/e69zI7vV3xJw/SjNoXCDvmAutaDEURcOD9Lw/WBI2J4aMRY8xofBwnPb+VdIapkxkSrxtJ0vV8qUc9sdWfeBjkmhfb1Lsa6jl1ozPhkletR58oiDNqg7Vyiz+DPYUti6IY0Cnox1MCpPZ5YCzSf/cRkUW/nCJjbCRSyD/79nFM3faVB6Q9JPbumUP9uwqh+imLS7DGlWFZBsbYoXF9KyZF0VonqcBgohERCRg1DGGMWxQx2xl0TAAYOOJwXMQZERWbePwC8eMnnyha/IkD1U880uU3YTWfCCS2CZuqG8dHOI54+JMeaocij5SR+GuHeq5RhG9ZFNF/9k1KCAn60RzIK2NiRMqiL87pj7Vvd441nofqBlPlYQ8Oib17ZtS/p4RQdZdAUSSB8bmWeWpSFKHcXr58+ehbJ92Q0agYsRDBBMStV5D4R6sohNdlR6AMwkNVtd3IVz9pqkGdmmTm1q2XRbwuikhXRRX5HjuJ3Jo1367dsiia6hf0IfrWVD/sRNRPQZ+PKOl9eIratzvH9OdDdQtT5fWxvzXSv+cIoeoQM+e6t99+e/crsiYmRRGsXcl3Q9YNbvzEwZBmpQVH2pHBx4gSl/3Ji98qtiCGOWWE6u/1w5/JIRxTt15Wzb/Wp+eHWzM//PDDw/vvv//oWxdPURSF9M19YB+IU/t5hXAmW35xjCPcIbHVx07lUJ3CobpVpsojjH1bhb6d//jjo6YjATRyzA3nOgTYmh/RkKfJXlHEBLvmV/O7IeuiIf5T7leSL8YcQYGQ6cIi+U2VCb1+5NeX4o+pWy+r5p88RnVdO7TL2l7FD09ZFAFxah+uZBVmaj+wUpl+i4tIP3RLal++c+o9p26VqbiEsW+r1P7NSi0fN50jkC4BwsiVIlkbe0UREzb/g3boz2GXohvXLhqqn7h1tQejObUaE8ibdBEuMZoJzzaTOjDAMaDJoxva7Cc/SB2OqVutX82/ThRs11uANc3aoC3ogGt903HLoqj2xUDfC/Rt4oQ63uhT9DP6NH0Qlz7LL+nm9GHGTsZPpfbtThdFvbx9datjtzJVHu3RV4m3xFT/PiSQLoGiSNbIXlEEX3755cMnn3zy6FsXMYYYObYxeDHa0P0YX+Lj2K7P8QQMIPsxqtV4RtD0cMCfcAxyyqSMGOOQVyN7HebUbXQ8yb/WibBapzUbdfoWfWytbFkUdYEC6YfpO7X/4q99O/219lsgDf7e92HUh5MOECGUQ3p+6z62674IuF4e8fBXl3wom7RhX3lA2JYn7jn9eySQLoGiSNbIQVHEgCBCv6IUOReuzPlX/LWuEsGWRVFWQEcCXP59IVQF1BY5tn9HIF0CRZGskYOiCLi6uvYfdcrTgwln7Q9ablkUQVZK5HX2rXZtiSX7t6JI1sgsUQS8Pjl1j1/kWOhL9Km1s3VRJE8bRZHI68wWRVzRe0Upl4LbZj/99NOjb70oimTLKIpEXme2KAJuodWHJEVOgYd71/yph4qiSLaMokjkdY4SRTxkx0f27MhyKve24qgoki2jKBJ5naNEEfDNIp4FIaHIMdzD22YdRZFsGUWRyOscLYrgl19+2U1ua/2oo6wPXv+mz9ybmFYUyZZRFIm8zkmiCNb8f1WyLlgZwgDew4PVHUWRbBlFkcjrnCyKgP+romO7YiRTsEJEH0FE3yOKItkyiiKR1zlLFAGdmmeMuKUmUsm3iO5xhSgoimTLKIpEXudsUQRkwvMia/86sdwO/gqAPnHvfw+jKJItoygSeZ2LiCLgFhqvWvc/mJSnB98g4s8j7+ktsykURbJlFEUir3MxURSYEFkhsLM/PXhuiNtlWxLGiiLZMkuLoi+//HI3V+h0a3Hc5bioKAKeIaHD83aa/5e2fTjHWz3fTBgfffTRcPDoLu/+9a9/DcN113H07aVEUV7U0enW5nJhfzFRFLJywMC792dL5O+wzMgtU84xBnaL8JzcaNDoruO4QnvzzTeH+3TXcT4LKjLm4qIocEWAseM5E5amtvCsyVOFZ8c4n5xLzqkGVS7JkrdzREQqVxNFgBBCEDGZPn/+fLfCwJ/LuoK0fjhHnCtuj7148WK38se5FLk0iiIRWQtXFUUdVhj41/2XL1/uHsxmwo1BZCUi97zl+iBY0960fc4D54RbY5wjztW9foBR7of0PRGRpbmpKKrwYDYTbgwiKxG53/3s2TPdlR0rd2nvPHiJ45z4oLzckvQ9EZGlWUwUiYiAokhE1oKiSEQWRVEkImtBUSQii6IoEpG1oCgSkUVRFInIWlAUiciiKIpEZC0oikRkURRFIrIWFEUisiiKIhFZC4oiEVkURZGIrAVFkYgsiqJIRNaCokhEFkVRJCJrQVEkIouiKBKRtaAoEpFFURSJyFpQFInIoiiKRGQtKIpEZFHef//9h2+//fbRJyKyHIoiEVmU99577+HHH3989ImILIeiSEQWRVEkImtBUSQii6IoEpG1oCgSkUVRFInIWlAUiciiKIpEZC0oikRkURRFIrIWFEUisiiKIhFZC4oiEVkURZGIrAVFkYgsiqJIRNaCokhEFkVRJCJrQVEkIouiKBKRtaAoEpFFURSJyFpQFInIoiiKRGQtKIpEZFEURSKyFhRFIrIoiiIRWQuKIhFZFEWRiKwFRZGILIqiSETWgqJIRBZFUSQia0FRJCKLoigSkbWgKBKRRVEUichaUBSJyE1BAH3++eev3BtvvPHwz3/+87Wwv/766zG2iMjtUBSJyE1BFD179mzSvfPOO48xRURui6JIRG7Oy5cvh4II98UXXzzGEhG5LYoiEbk5H3/88VAQ4X777bfHWCIit0VRJCI3Z+oWmrfORGRJFEUisgijW2jeOhORJVEUicgijG6heetMRJZEUSQii9BvofG9IhGRJVEUichi1FtoX3311WOoiMgyKIpEZDHqLbTff//9MVREZBkURSKyGLmF5q0zEVkDiiIRWRRuoXnrTETWgKJIRBaFW2jeOhORNaAoEpFF4E9fWSH6z//8z4ePPvro4eeff37cIyKyDIoiEbkpf/zxx8OXX365+3d8Von4NtHXX3/98Pbbbz988MEHDz/99NNjTBGR26IoEpGbgBj6/PPPd2Lok08+Gd4y++6773Z/9cGD1zyELSJySxRFInJVED+IIB6oRhQhjg6BIEIYsXqEUBIRuQWKIhG5CtwW41khVoa4XcYzRMfCc0bcUnvrrbd2t9hERK6JokhELgpCJmIIIXOKGOr88ssvr/Lk4exL5Cki0lEUichF4AFpVnW45XWtVR1Wn3g4O6tPc27FiYjMRVEkImeR5394QPpWz//kOSXEEc8p+Z0jEbkEiiIROQkEEKtCS74pNueNNhGRuSiKROQouDXGg8/cKlvLBxd5xojbabzhxrNH3GYTETkWRZGIHCRfn2ZFBtHBg89rBdFGPT/88EO/ki0iR6EoEpFJRl+fvhe+/fbbV1/J9kOQIjKHxUURRheDpbuu83aCHAPP5mzlWR2efeK5J9wPP/zwGCoi8nduKoqYnDGwGCfu/T979uzhxYsXrwyW7nqOyY32fv78+c7PLRAmC7/3IpX+VteWXnnH/rz//vu71SNWkUREOlcXRUy8TMARPyzFY5x8S2QZEEG0P89dcFuB88Lvtb4rI/dBvj7NxcqpX5++F3jOiOeNEH72exGpXE0U8SE3RFAmXD+ytk6Y/CJceaOIbXk6PGWB8JSEoIjM4+KiCEODEOJDbqxIyP3AG0WcO8Qsola2C2OTc+2tpG3fMhSR47ioKGKVASPrasN9w4SJqOXqWbYFDxojenGO09epD5d/9tln3uIXeYJcTBQxgXLl6VXWduDqmdsL3la4f3LBwoPGruDuBxv2xRdf7MQRY8A3N0WeDmeLIiZMvl+C8ZDtwXMmrCoodu8Tzh+TOxcsfsjwOLBtXOzRflwcKI5Ets/ZooiHNPnSrWyXPDSvMLoPnMwvj+JS5GlwlihiiZl777J9eBaFWy+yXrztc31yG5KLBG9DimyPk0URb6ywSiRPB1YfvE26PnxA+PYgiBBGvJDgA+si2+EkUZTbKT6A+/RACPs14HXgq+TLgy3Mpw2e2neeRLbISaIIA+B99acJEzHn39WI5eC2mB8dXBfYQ84JAhVx5DkRuU+OFkXH3DbjAWyecYibO5EquPbDlemxXLJNvY22DJxDzn0m3m+++ebh119/fdz7sNvuLz0w7i5B7z8IgFr2KVwij7URwco5UrCK3B9HiSIGOIN9rrh59913d0Ycw/zpp58+vPnmm7MeTiSeTHNK+1yyTY/tB3IejJn8kWl9fiXjKmSMBc4PY/AS9P5DvucK7UvksVZoey4cWM3z1qbI/XCUKDp2hQCjV69UMdrVz1UiYQgnrnqBK10MMPESl7A///xztw3VTxy2CcMxgeDYJl9+90H6Ho9tjBp1Sh16OfD999/v0vYr3lHcDnmTjrhJyy9h5EneFfIhnPh9giJs37ES3tsUCOcc1Yl1Lj5of30QQDy7h+Ptvw79pYoetukHERrpY8C5zlirfQASj/2ji5ZR/6Es4ibP3l+Jt69PAnlUUUSajGvyo0641Im8qhBnux9LpdoWHMQ+hO6nDNKQNmVlLKc84vfjwl9tQEAMIYoQR9jOWn8RWR9HiSJWB455zRejV40WBi7GBOPAfowfhjHGi20MML8xmN14Vj9xUw5580sY2xgvtmMQO6QjHnlhBDOBEI4jLPXv5eDwk5ZjYH8MXo/bSVmkzYSEQSVd2oP0mWh6fOIF4o2OocI+0vCLA+IlzxzLMbBahKH3CvjyIDhZFWJM1Al7RO0LESHps5zf9H3CyCv9hLhA/PQD9rOvw77ef+gvOPJPn0z/n9MngXg1v9SVfp991IntCJKaF3nnWDukYV/qgB8Iq2mqP3UlDXVJGvwcH21G3NiVKnDqeRjBeOGiEhtKGX4uQWSdzBZF/Fko/6J+DBiVGBJ+Y4gBA4TD4HQj1A0M4cQJ1U/cXF1CNXLQ/QHDRn1SPi7lkn8ESejl9DpVA93jdvrxQU0PmQygx49/3zF0pvII/XjmgHH3jZvLkQ8EsgI391xw/ukHOPpPXT3Kb2Bf4uX8sz0lWiqH+kv8x/TJpKH8CCIgPBcH2U89ESE1L7arMAnUoR47ecRPPrhQ/eSXMpOmbldSJ4iwnAPiKOeZNOQtIuthtijCABz7oUYMSYQP293wxbDEYVxgrgGGHjd5he4PhGG8s7/G6+VBL2dfuX1fJW3R6WUmHr9TZVPe1DF0ah6jOoyO+RDeQjufrCCw6sZ4OHYFIRNyxhlwLhEL9RzTT9JXGIe1PxCOn/h1jFZ6H+z9JX7yn9snSYPreePv6RE6wLFSRxzbI4hPvqH29+QX4ifOqFwubmr6UNuX35E4OwTiKCuCvNovIsszWxTxkbJjBy7GAsMCMTr8AkYcN2KuAYYeN8YsdH/oV5OVXh4cqlNd6elxO6P9NT3U+vX48e87hs5UHmF0zIfg1hmTORO7HEeeNWHF4JxnTXLbtfaDiKSMr8SpdD/Qn0bhcKi/xH9MnyQeog5X07BNPiOS/5w4gXrF3+1B9U8de01fQczQ1vyeA8+OYV/9SrbI8swSRRjwFy9ePPrmgyGpBoglcQwPRppJgG0MIkanXvmRjvAYCMJxxOP32bNnu23ohmyf0etQDhMHeVFWjBvhyT/0cmLIU3f2Z2LrcTuUmePJcWfioo1i1HMLL8aX+PzW/KeOoUO8qTalfdh/CqeI5XPg2Hv7cswcxz1AH2HFFTGEKDpVDFVoj3r+0h9zrgE/4ekjaUP6AOG0Xxcnld5/8Nc2r3625/bJpKEe+CF2IrfQ+hgmXuJOQXrScGyUn/j42aZe7E88oM7pS7iMD9yoPPLCFtV2PgfyQRj1twxF5HbMEkWnPE8E1YgGDF0meyYEDBKGiF+EAfCLP8YKYrBIS755ZqfGAcqrZXZ/h7xSfuLV/EMvB6gLaTGeqTuM4nYwqKQjbj1uwnKclRw/dez5j46hM2pT0mHsyftUqO/c54roRwgB+tKpD5oyiaXdAsfAxFXp/qXheFkRQgxR90s+oM55rOedvlvbB9hPH+FcZ9xB+kX6zxS9//QxMvIf6pOjNBkLpKGunOsejzDGzz5yjDjGUhU1qRu/lFPrl3FJ2SmDsonboYyRWDoX+i71Y5z4zJ7IbZklivwzUJmCVY9MlCOqEOKqOu4cUcTEyW8myiqKqAv+uKXFEcfJJIsY8mN+54MQ4dwfA33gGuIlgu1aMHbsOyK3ZZYo4mqFwSnSGX27akoIVXeOKALET/pkxE/EUmAF4BqT4Rxytc+E5tX+5WD1Zp8IH0G/uIb94vzegmuuMorI68wSRUxwOJFO3kCbI4SqI11uXcx1lFFFD9u5hYEI4aq9T36Jz3NPozwv7f7nf/5n+PVpkXOhr9fn0Vw5Erk8iiI5C26t8rA1wuj58+dDATRyedvmGPff//3fr4mirBbNEUXUb5TnpR3l8VKCgkiuAatErBrRx3yNX+TyzBJFo1sk14bnRerDlddaAj8H6pMHQ5mQa32PgQm9ck5et6beWuXKNStHhwTSubfPAoKIsDXdPmOy4tYKq0XeOrs8Oa+c64y/EYwjHJCGh6dJl7B7glUibLD/pSZyXWaJoiU+0scqQH12gElviQluH9QnguYcIdMn+nsSRVOriIcE0qVEEc+YRBQBfYbzkkmzC85bQtkIxjxX5O2Oy1DH3T6qDcnbZDknt3oe6Fx8UF/ktswSRVz5crvjViAIMFo4jBpXeRgzjGFeg8fIVQjHeOCIP4Jw8ku+9Soz5SQPrsyAMPJGqJAur+nCPlFEPPJJOexL2dQ9r9yTjsmbfbiE1bzwU1Y9ZvYTPtUet+Ljjz/e1WMfI4F0qiiay5xJ81bc08RGf6cvUd/09fS10P1Jg2M7EId8+E1/nhqnGS+jPDJeGZP4M16SL7+h+uuYqtA3urheG9SR8aKgFrkts0QRRp3BeUtiZDEOGDp+mUwThqHEiAKGNgIFg8p2RE2FeLla5LcaRrZjqDGk5AFssw9Dm7wzWaTMvk0dySvlsI0wIo/UkTwJw882v0lf8yJt8iJ96oV/qj1uCeUe8/xMBNJTXP5f+y0Q+mPti+lPbKffQfVn7NGncfQHIIyxQ9yIk6lxSr9OHyecPg3klTHDOMrYTR0zflIXqP6U26GslLE20obcemWciMhtmSWKgAf7bmnEu0HbZ/z4xeAShsPojYwhYISJw/4YV2C7kn29HhjmWm7S1+2eV4U4GHoMX/Lt8ffllX241AO6/1Yglq+96rM1GEeIItoOkTQS8EtAH6K/9frs62v8MpFXIng6hI3GKS4CrEJ8xkqnjol9detjFxh3o7KWhhcW8rC+D+mLLMdsUYQhueVDo92g7TN+GMnEj+uGGrg6JA37s+pCPjAlPpJfqOXW9Nmu+ytMBpRBHcivCrepskd5Te2bKveaUCZXtHIaiCNupyGOuA25BnGZMYLLasq+vtb7LtCvRysx+8YpYoX95JuV2JSTcPxQy9xXt5QRetw1gABiDPEZh5HNEpHbMlsUMXgxXLfikEGrfn7nGBSMab0KJh35QDW0wD7i9nrkdhjU9PvyAtouxh5qvqOy99WLfTi2Q/ffgqmHrOU4uKWISEcc0bf4HtPS0Pfpe4f6Gr997OEf9cVR3A77R+MnFzFQ9++rWx+7wC23NcAFJucbu0CdRWQdzBZFGG5uod3qgb/cpsJgHHp2IKswWZofGUPAAHEFSxwmH57JYRtIj+HNvggf8iEdxpo6ES+GNfXr25RBevwRUcmHMLbJJ3UkLWVnwqh5pS5Jl2PGn23o/ltw6z+DfQowWfIBzCUmS/oXjnIzphBHEUj05YzL9DXisU3fxVFvICx5Jd+pcUr/Jl/CqvjJuCOcOFl9Io+wr27JPzBuyWcpsJ1ZGaQe3nYWWR+zRRFgpG55vxsDiVHDMO57ywSIEzFCOPs7GFD244hf42FYKYs8ar4x2tlXrzRr+l4mBjrpkoZt2pB9lI8D9rMPBz0v/Bj6TArA/lrP7r82Szx8/5TIbRWeMUk/uTbpQ/TR3tcRMjW89rWMPVytK3H6eEzcGp7+T/4ZA5C4hNfyahyYqhvpa31uPUYCt0mpM+OFZ8gUQyLr5ShRxBXbVp8hqVefFYxZN8Ly7y9E+3bM9WFSRxixKucDuPcFF2H5Ww5+8YvIujlKFMFWJ0OuNEdwZbnE1eWa2bI4Xit+Jft+YCWIFSHEEBdUa/v0gohMc7Qo4mqHwX6rZ4tkfTA5u2qxDHm+hjGIWHccrgfEUM6NX58WuU+OFkXAUrC3lJ4mt34LUcYwAfMafyZgVyOWA6HKmOBcuIonct+cJIq4AvK7Gk8PJmKebfHZiPXAucitmjV+JXvLYP+wg9zSdOVUZBucJIoA44sx8E2KpwFCGEHEVbGsD8Yjooi/EFnTV7K3CAIoX5/mS9Qish1OFkXAR+aYKL063T6+bXYfIF79Fs514NYYF4LcKnOVXGSbnCWKgCslhJHGd5swySKI/HL1fcF5y1eTEUeu8J2G7SjytDhbFAGGAmHk1423RZ4hcoXovqkrHI7RebjiJvI0uYgoAp5h4B47hkTuH1YAmUi9Mt4OPAuDyL3lV7LvDZ/NEnnaXEwUAVdXGBImUx9AvE8QQUyavFXjs2LbJF/JZpz61tS/8S0+EYGLiqLAxMqkiuF1peE+4PYAzw4xUbqK8DRgbHJLjT+gfarf16Hf+70nEQlXEUWByZVJFoPDVZiT7bpgUuSqOOfIZ4eeJrxFmi8xP5WvZD/FYxaRw1xVFAWuxrgKY+XoxYsXu1+uzpiQCUcs6a7rWAmgvfkaOe3PZIAYIszVPIGnsGqS/5B7yqtjIjLNTURRBUPLJM3VGRMyK0hM0rrrOq6KaW/+noX2920amaI/X7OFh43p84wDHjT3OSoRmeLmokhE7gMuYBBFiCNE0j2KIwQQK6IIIoSRiMg+FEUispd8s4fX1O/lmz3cGuMWGbfKvD0sInNRFInIbBAbrBzxpuLaxAbijdvy1A/xxsPUIiLHoCgSkaPhTUVuS63hf8C4zcdKFmKIB8V9Xk5ETkVRJCIns+Q/xvOM070/8yQi60JRJCJnw2oRH2xl9ejab3f1t+P84KKIXApFkYhcjHwlG8Fy6e8AcVuMZ4V44JvbZX5wUUQujaJIRC5OBAzi6FwBg9Diwe5rCC0RkYqiSESuBuIot7r4cOgxt7q4JceqE7fk/AsaEbkFiiIRuTo8B8RfzOQ5oH0PRfPAdh7e9uvTInJLFEUicjP2fSU7X5/mgW2/Pi0iS6AoEpGbU7+SjQj6z//8T78+LSKLoygSkUXh7zh8ZkhE1oCiSEQWxT9rFZG1oCgSkUVRFInIWlAUiciiKIpEZC0oikRkURRFIrIWFEUisiiKIhFZC4oiEVkURZGIrAVFkYgsiqJIRNaCokhEFkVRJCJrQVEkIouiKBKRtaAoEpFFURSJyFpQFInIoiiKRGQtKIpEZFEURSKyFhRFIrIoiiIRWQuKIhFZFEWRiKwFRZGILIqiSETWgqJIRBZFUSQia0FRJCKLoigSkbWgKBKRRVEUichaUBSJyKIoikRkLSiKRGRRFEUishYURSKyKIoiEVkLi4qin3/+eWcMddd1ImtGUSQia+Emouj3339/+Oqrrx4++OCDh3feeefh2bNnO/f222/vDKLuui7t/dZbb+38X3755cMvv/zyeHZEloU+qSgSkTVwNVGEEPriiy92wufly5cPH3/88cN333338NNPPz3GkFuDEGLy+eSTT3YCCcf2b7/99hhD5PYoikRkLVxcFP31118Pn3/++W7C/eyzz3a3yGSdIJJYNXrjjTd24gghK3JrFEUishYuKoq4RcYEiyj6448/HkPlHkAcIWQ5dwhbkVuhKBKRtXARUYQA4nkhbpG52nC/cB4RRTz35S01uRWKIhFZC2eLIiZPjBrPC8k24JYnz4L5/JfcAkWRiKyFs0QRkyarCj43tD1YNWKy+vrrrx9DRK6DokhE1sLJogghhCDydtl24dmijz76SGEkV0VRJCJr4SRRhBDyuZOnAcLo/fffd9KSq6EoEpG1cLQoYpL0ltnTgltpPGOkCJZroCgSkbVwtCjiLTMfqn565Hapr+vLpVEUichaOEoUIYYQRfI04XV9nMglURSJyFqYLYoO3TYjPO7XX399DJVTYIJY4yRBH+DjnD5cL5dEUSQia2G2KOKLx/wVxBRvvvnmw7vvvrtzbM9ZUUJAufL0d/jPONw1oL2nhO0cDvUDkWNRFInIWpgliuasECCE6mTb/aT95ptvdi6wjYiqq0t9wq7+GufPP/985ceg8hcj++rX0wJhpOsGOXWt4Ukfqj91rMdHHvvyTrwRXRRRVuoMo+Md1a+mAfy0N2XXdk1d99Up+KC9XBpFkYishVmiiO/U8L2afXQRFLEDTND4meg//fTT3TawTTrCmZQBfyV+8mKbtDjiZ5u64XraMEqLAGCbskmbFavvv//+VTj1SzhhU8dH3sTLsbHNbz0+mGqHDvuTJvUMyZf9bDOZIGr6seOvogmIS3jSI5ISdqhOFeLyZ78il0BRJCJrYZYo+vDDDx++/fbbR98YJtasQCAy6uSKSKhGj8kXYULcPgmPJneIsKmrH6RFxAT8VbiEqbTdj2iJOOj0vKu/5k0Y+wLHHf9UO3SoA468IsqAuLVuHHv285uVHn5rusroOGobcu5Gdarwaj4rhyKXQFEkImvhoCjidsnLly8P/us9woAJG9cFCH4m6Tgm4lNEUY87muCrP/S0+Ed1QhTh2IefY8EP+8qqdU7eoZY9KnNKFGXlq672EB9X8yAe7BNfFeLU46h1hQiyQ/CP+r/88sujT+R0FEUishYOiqIffvhh90XjQzC5ZrKtt6OAfREXlSoYQp+k4x/F7RN894eednS7qZNVo8TbV1bNi7DuT9mEj9qhQ7mkQTDVeh9axRml6bCvHkev01xRxO2zOfFEDqEoEpG1cFAU8aYRbxwdgsm1TrZ1FYPJE39WjzCA3LJhMiYd4dmHPwaSdPihiovQJ/juD6O01KfeiuKWU4RQVmdIl/KJH0FC3Hq8iQM1DdSyp9qhQzwc0IZJT3zyjoihnokH1I/9uY02oh4H0AY5T+RHWXMmqLliWU6jjo1AWO/HW0BRJCJr4aAomvM8ETDZZrIOhFWBg0HH1XAmZfyZmCM4iMfkzT4g78QJ+GuZ3R9GaYGw1IltJqCUSR34jfAhD+IRHiGRslJHIKz7a9lT7VChDl24xJ/niFK/Go/6E76PHAdpU3/yT5vvE1SVY54r4hYsfYi+9PHHHz+GyiGqYAXOTxfR6Z+dnNt7QFEkImvhoCji9euffvrp0SdrBsHFRHoLEDrPnz9/9P2dKoSI9+zZs52rk7wcBiGEYED81rZDvLIvLmKph1fRvFYURSKyFg6KIlYD/CPQ+4DVnvpg9rV58eLFaw/gTwmh6hRFx4HYQdzUc5vbnKH6+a2rR/X221pRFInIWjgoipjIREbwBtr/+3//76AQqo7nkJgAdYcdIhMQOnUFkNWf3D6Ny21T4hGfVcOII0TTKP+1uLfffnv3KyKyNIoiORlurfKMEOKoi58px+cdWBnQHXZZGYrICf1WWgcxlGfXEEk8FD/Kfy0OoZxjFRFZkoOKp98iuQX94VEmgDU9OFofns7D2adA2np745y8lqDeWuWbRfyD/iGB5O2z4+miCAHRb5VmzNSxk1tvIiIyj4OiaImP9OVWQMCwd6G0JNQlk805QoaJrk529yaKEDkj9gkkRdHxcHus9hOob2nGQfXj1jRuRETWzkFRxNI2y++3AlGAsa+CAePOMwfcCmCC6K8lE4/wfYKCOEzIuBqPfMmPMPKor6SThv1JlyvzfaKIOCkrebE/dc8xkQ5/wiin55UVKeLUYyYO+6ba4xZwnNwKO0QXSByPXI6pFdQ1rayKiNwLB0URkxh/CHsrEByIIn5zlZurXkRGrpAjUAhHJBAXkTA16bIveSIkIj4QJOSHH2GScoBw8iOceBFC5DHapk6kIT3hpOU3ooftCBlgO/VCENW8mNTIC8GT8IgftnGj9rgV1IkHZI8BgXRLgS0iInIMB0URX7Pmq9a3hEm+ggBgEg7xIzQQGGzH9bQV9iMiECvkAQgWXGB/9vW88KecxKnbETkjEDkRV8m3l93zqvsi2IBf4obuvwUI5SkBKiIico8cFEVL/CP6XFGEaEAURVzEdVhFIQ2TOPtZFcIPPU0VJlP1qHHqNr+jW1nUMfXMyg7sKztlhX37uv8WcDzffffdo09EROT+OSiKgNskt5x054qiunqyD0RQXdUgbdJ1YZKVJBjVA4FV09ft0UpRboNV5oiiNa8U8f0c3krMd3RERES2wCxRxIOyuFvBJJ9nfOKfEgFsIyDwE58VjA7hCBF+c3sMBwgP0mQf8fKQKtvUg7wRShFL+JO+bk89U5Sw1A8/pC7EocyaV32miHSE12eKiBu6/9r4Z7AiIrJFZokiJtxjH6o9BwRBXUVBmPAgchj5c3sKATECAUIc4pI/v0AaxAu/iKsIIkCUJDzxob4l1t8YQxglT8qEiCHyyf5AWvzE6XlRF/Ihbb0tR5x97XFtqNMtH74XERG5BbNEETAxb/EZEgRJFSmVrOjI/4JA5kvW3joTEZGtMVsUbXUyZJWlrs5UEILyOlsVxyIiIrNFEfBqPq/oy9Pk1rdRRUREbslRoojnYfgy8a3/C03Wwa3fQhQREbklR4ki8M2jp8lnn302+eyViIjIFjhaFMESX7mW5fj2228fPvzww0efiIjINjlJFMHHH388+YCybAffNhMRkafCyaKISZLbaD54vV34dhLPEfFXLyIiIlvnZFEUuI3GqpErCdsCsfvee+/5UL2IiDwZzhZFwG00JlBXFO4fRBDPD/nMmIiIPDUuIorgp59+2r2uz2Tq6sL9wUofb5e98cYbuwerRUREnhoXE0XAxMptFyZWJlhvqd0H/I8Z54zX7hW0IiLyVLmoKApMrEywz58/3/0tBLfX+PCjrAPOT16zf/Hixe4PXj0/IiLy1LmKKKrwP1k8iP3y5cvdm0y8sfb555/vHPt4w+meHP9WPwpfs0t7I4J49gshxDbCyJUhERGRf3N1UVThmzd8ETuTNKtITNL34nhmCnE32rdml/ZGBCGSRERE5O/cVBTdOzx7w60mERER2R6KoiNQFImIiGwXRdERKIpERES2i6LoCBRFIiIi20VRdASKIhERke2iKDoCRZGIiMh2URQdgaJIRERkuyiKjkBRJCIisl0URUegKBIREdkuiqIjUBSJiIhsF0XRESiKREREtoui6AgURSIiIttFUXQEX3311e4f/0VERGR7KIqOIP82LyIiIttDUXQEiiIREZHtoig6AkWRiIjIdlEUHYGiSEREZLsoio5AUSQiIrJdFEVHoCgSERHZLoqiI1AUiYiIbBdF0REoikRERLaLougIFEUiIiLbRVF0BIoiERGR7aIoOgJFkYiIyHZRFB2BokhERGS7KIr28Msvvzy89957r9wbb7yxczWMOCIiInL/KIoO8PLly4dnz54NHftERERkGyiKDvDxxx8PBRGOfSIiIrINFEUH+PHHH4eCCMc+ERER2QaKohmMbqF560xERGRbKIpmMLqF5q0zERGRbaEomsHoFpq3zkRERLaFomgm9Raat85ERES2h6JoJvUWmrfOREREtoeiaCb1Fpq3zkRERLaHougIuG3mrTMREZFtoig6Am6beetMRERkmywmin777bfdbah7cl9++eXOjfat2dHWcpi//vpr2H463RYc/ftWjMrX6dbuGCNXFUW///77w1dffbVbXckfqOa5nP7HqrrrOdqaNn/+/PmrMM7J119/vTtH8m+++OIL+6Vuk45+Tf++BVw4Oo509+beeuuth08++eTyooh/jWfwvf3227vnb5h8EUZRYrIcdSWEc/LRRx/tztE777yzM2RPfUXp888/3zmRrXHLvu04knuERQLmxIuJou+++26ntHCfffbZw88///y4R9bOTz/9tFPIXN0hZjmXTxGNuWyVW/Ztx5HcIxcTRaw6sNLwwQcf7FaJ5L5BzHIuWU5ELD0lNOayVW7Ztx1Hco+cLYqYPN9///0nOXk+BarYfSq31TTmslVu2bcdR3KPnCWKeB6FCfOHH354DJGtktui33777WPIdtGYy1a5Zd92HMk9crIoyrd6bvl6pywL5/rDDz/cPXe0ZTTmslVu2bcdR3KPHC2K/vjjj92tMlaJ5GnCG2rcTqMvbBGNuWyVW/Ztx5HcI0eJIp4f4q0kX6kXbqdx63SLzxlpzGWr3LJvO47kHpktivi431YnQTkN3jJEJG9txUhjLlvlln3bcST3yCxRxLMkvl0mI1g15O3DLT1bpjGXrXLLvu04kntkliji4dqn8NaRnEb+wmUraMxlq9yybzuO5B45KIr4qw6+TH0uPJh7if/cyUcFgcn41Ae+WeGoz0b9+uuvu4bYKn/++edVH47njTQewN4CI2NOX6Hfvfnmmw/vvvvurq9wS7n2x3P70KFztMY+Sn1oj+pyDH1fPTbaru6nDdPGNU0ccUk/5/iJR1sey6j9Od9b4pZC5ZZliVyKvaKI7w9xa+Rcvvnmm1cTyrkwCWEkASN2ivEDBFoXaUw6W6W227XgFmsVmvdKN+bff//9ru/yG9J/erue04fmnKO19VHq++mnn+7qHseYZLwTHugX8SOIaM8qQLARGKLkgZ848XPctPecPkwc0hwLaXr+iqLTuWVZIpdiryjiIdpTjEsHQ4NRzG/AKGIoI5jq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sja0SiKyCIoiEVkbWiURWQRFkYisDa2SiCyCokhE1oZWSUQWQVEkImtDqyQii6AoEpG1oVUSkUVQFInI2tAqicgiKIpEZG1olURkERRFIrI2tEoisgiKIhFZG1olEVkERZGIrA2tkogsgqJIRNaGVklEFkFRJCJrQ6skIougKBKRtaFVEpFFUBSJyNrQKonIIiiKRGRtaJVEZBEURSKyNrRKIrIIiiIRWRtaJRG5CZ988snDe++998ohiqr/448/fowpIrIMiiIRuQlffPHFTghNuc8///wxpojIMiiKROQm/Pbbb0MxFPfLL788xhQRWQZFkYjcjHfeeWcoiN56663HGCIiy6EoEpGbMXULzVtnIrIGFEUicjOmbqERLiKyNIoiEbkp/RYafhGRNaAoEpGb0m+h4RcRWQOKIhG5Kf0WmrfORGQtKIpE5ObkFpq3zkRkTSiKROTm5Baat85EZE0oikTk5uQWmrfORGRNKIpEZBH8rzMRWRuKIhG5Kb///vtOEP3Hf/zH7he/iMgaUBSJyM348ssvd3/p8dVXX+38/OInXERkaRRFInJ1fvzxx534+eSTTx7++OOPx9B/g59w9hNPRGQpFEUicjV4kPqDDz54eO+99w7+Cz77iUd8H8AWkSVQFInIxfnrr792f/LK6s933333GDoP4pOO9OQjInIrFEUiclEQNW+88cZZoiaiiny+/fbbx1ARkeuiKBKRi/Dzzz+/uv11qTfKyOfDDz/c5Uv+IiLXRFEkImeRB6Xffvvtqz0oTb7kP3pQW0TkUiiKRORkeKWeW1y3eqWecm5Znog8LRRFInI0Wbnh44u3Xrm5xcqUiDxNFhNFP/30086gccXHA5UYOZ4b0O13n3322a69aDfaj3YUuRVresbnGs8wicjT5maiiLdJeCvlo48+enjx4sXDO++8szNoiKE6yev2O/5VvIpI2vHly5e7K/ZjX30WmQvjl763xrfBLvG2m4gIXF0U8UE2riwRQlzRff311z4oeWG4SubZDtr3+fPnO+Hpx+/kUuS7QaxSrlV05BX+U76LJCISriaKmKhZzcBIcWXpFdxtoJ0RnrQ77a8AlVPhgub999+/qy9MH/MFbRGRzlVEUa7YfENkORBHtL9v6sixIKRZFWIM//DDD4+h9wW3mr0wEJFjuagowvhwhYYo0hCtA84DEwNXz67W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)

Figure 3: Server-side data copy of an entire file

###### Application queries the Copychunk Resume Key of the Source File

The application provides a handle to the **Open** representing the source file.

To request a Copychunk Resume Key for an open file, the client constructs an NT_TRANSACT_IOCTL FSCTL_SRV_REQUEST_RESUME_KEY request, as specified in section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17). The Fid of the source file (**Open.FID**) is placed in the **FID** field of the client request along with the FSCTL_SRV_REQUEST_RESUME_KEY function code. No **NT_Trans_Data** block is required. The request is sent to the server and the server's response is processed as specified in section [3.2.5.9.1.1](#Section_6ac8b081122d458db07541d5cf452d5f).

###### Application requests a Server-side Data Copy

The application provides:

- A handle to the **Open** representing the destination file.
- Copychunk Resume Key of the source file.
- List of source and destination offsets and the lengths of data blocks to copy from the source file.

To request a server-side copy of a data range, the client constructs an NT_TRANSACT_IOCTL FSCTL_SRV_COPYCHUNK request, as specified in section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17). The Fid of the destination file (**Open.FID**) is placed in the **FID** field of the request along with the FSCTL_SRV_COPYCHUNK function code.

The NT_Trans_Data buffer of the request is constructed as follows:

- **CopychunkResumeKey** is set to the application-provided resume key.
- **CopyChunkList** is set to a list of SRV_COPYCHUNK structures (section [2.2.7.2.1.1](#Section_e4e201827c714755b638bb75ff1c06ca)), where each structure is filled with the application-supplied source and destination offsets and the length of each data block.
- **ChunkCount** is set to the total number of the data blocks supplied by the application.

The request is sent to the server and the server's response is processed as specified in section [3.2.5.9.1.2](#Section_b7ac17fe078a4a35b5bab527a9a9bcb9).

#### Application Requests Querying of DFS Referral

Processing of this event is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.4.44.[&lt;91&gt;](#Appendix_A_91)

#### Application Requests Querying User Quota Information

The application MUST provide:

- A valid **Open** to a file or directory on a share. The quota information on the object store that underlies the file or directory is the quota information to be queried.
- A buffer to receive the quota information and the maximum number of bytes to receive.
- **RestartScan:** A **BOOLEAN** that indicates whether or not a scan on the volume is to be restarted.
- **ReturnSingleEntry:** A **BOOLEAN**. If TRUE, then the server MUST return a single user quota information entry.
- A security identifier (SID) list, a start SID, or no SID. If the application provides both an SID list and a start SID, then the client MUST fail the request with STATUS_INVALID_PARAMETER.

The client MUST construct an NT_TRANSACT_QUERY_QUOTA subcommand request, as specified in section [2.2.7.5.1](#Section_9f3f73f99c4a42ba9f56e6352491d714), with the following additional requirements:

- **NT_Trans_Parameters.FID** MUST be set to the Fid of the application-supplied Open.
- **NT_Trans_Parameters.ReturnSingleEntry** MUST be set to the value of the application-supplied **ReturnSingleEntry** BOOLEAN.
- **NT_Trans_Parameters.RestartScan** MUST be set to the value of the application-supplied RestartScan BOOLEAN.
- The **NT_Trans_Data.SidList** field is set to either the application-supplied SID list or start **SID**. If neither were supplied, then this field is not included.
- The NT_Trans_Parameters fields of **SidListLength**, **StartSidLength**, and **StartSidOffset** MUST be set according to the following rules:
  - If the application provides a SID list (a list of SIDs that represents users whose quota information is to be queried), then the client MUST set the **SidList** field of the request to this list and set **SidListLength** to the length of the list. In this case, **StartSidLength** and **StartSidOffset** MUST be zero.
  - If the application provides a start SID (a single SID that indicates to the server where to start user quota information enumeration), then the client MUST set **StartSidLength** to the length of the SID and **StartSidOffset** to the offset in bytes of the **NT_Trans_Data.SidList** field relative to the start of the SMB header. In this case, **SidListLength** MUST be zero.
  - If the application does not provide a SID list or a start SID, then **StartSidLength**, **StartSidOffset**, and **SidListLength** MUST be zero. If the application provides both a SID list and a start SID, then the client MUST fail the request and return the error code STATUS_INVALID_PARAMETER to the calling application.

The request is sent to the server.

#### Application Requests Setting User Quota Information

The application MUST provide:

- A valid Open to a file or directory on a share. The quota information of the object store that underlies the file or directory is the quota information to be modified.
- A list of the security identifiers (SIDs, representing users) and their associated quota information to be set.

The client MUST construct an NT_TRANSACT_SET_QUOTA subcommand request, as specified in section [2.2.7.6.1](#Section_5172dc9ce7ad47fa86c0317b047a37eb), with the following additional parameters:

- **NT_Trans_Parameters.FID** MUST be set to the Fid of the application-supplied Open.
- The application-supplied list of SIDs and associated user quota information MUST be set as the contents of the NT_Trans_Data block, as specified in section 2.2.7.6.1.

The client sends the request to the server.

#### Application Requests the Session Key for a Connection

An application provides one of the following:

- An **Open** to a file or pipe.

OR

- An SMB session that identifies an authenticated connection to the server.

If an Open was supplied by the caller, then **Client.Open.Session** MUST be used.

If a **Session** is found, but **Client.Session.AuthenticationState** is not _Valid_, then an implementation-specific error MUST be returned to the caller that indicates that the session key is not available.

If a session is found and **Client.Session.AuthenticationState** is _Valid_, but **Client.Session.SessionKeyState** is _Unavailable_, then the request MUST be failed with STATUS_ACCESS_DENIED.

If a session is found, **Client.Session.AuthenticationState** is _Valid_, and **Client.Session.SessionKeyState** is _Available_, then the first 16-bytes of **Client.Session.SessionKey** MUST be returned to the calling application.

### Message Processing Events and Sequencing Rules

#### Receiving Any Message

In addition to the global processing rules for a client that receives any message, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.1, the following processing rules apply to the extensions presented in this document.

**Signing**

If a message is received and Client.Connection.IsSigningActive is TRUE for the connection, the client uses **Client.Connection.ClientResponseSequenceNumber\[PID,MID\]** as the sequence number in signature verification, as specified in section [3.1.5.1](#Section_2fa60c5a71ee4248a4e9dd5d0db2373d). If signature verification fails, then the message MUST be discarded and not processed. The client SHOULD disconnect the underlying connection and tear down all states associated with this connection. If the message is an oplock break, the signature is never verified, as specified in \[MS-CIFS\] section 3.2.5.42.

**Session Expiration and Re-authentication**

If the request passed a valid authenticated session identifier in the **SMB_Header.UID** field and the status code in the SMB header of the response is STATUS_NETWORK_SESSION_EXPIRED, then the client MUST look up the **Client.Connection.SessionTable \[UID\]**, set **Client.Session.AuthenticationState** to Expired, and attempt to re-authenticate this session. Re-authentication follows the steps as specified in section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953), except that the UID sent in the SMB header of the SMB_COM_SESSION_SETUP_ANDX request MUST be set to the UID that represents the expired Session. Also, as described in section [3.2.5.3](#Section_56560c32b01d4ed6907c7ebdcf7eef57), the existing Client.Session.SessionKey MUST NOT be modified during re-authentication after a session expiry.

If the authentication fails, then the resulting error code MUST be returned for whichever operation failed with STATUS_NETWORK_SESSION_EXPIRED and the session associated with this UID is removed from the **Client.Connection.SessionTable**. If authentication succeeds, then the client MUST set **Client.Session.AuthenticationState** to Valid and retry the operation that failed with STATUS_NETWORK_SESSION_EXPIRED.

#### Receiving an SMB_COM_NEGOTIATE Response

Processing of an [SMB_COM_NEGOTIATE response](#Section_d883d0a55a0a46268e3e87b0b66b79aa) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.2 with the following additions:

Storing extended security token and ServerGUID

If the capabilities returned in the SMB_COM_NEGOTIATE response include CAP_EXTENDED_SECURITY, then the response MUST take the form defined in section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4), and the client MUST set the **Client.Connection.GSSNegotiateToken** to the value returned in the **SecurityBlob** field in the SMB_COM_NEGOTIATE server response.[&lt;92&gt;](#Appendix_A_92) If **SecurityBlobLength** is 0, then client-initiated authentication, with an authentication protocol of the client's choice, will be used instead of server-initiated SPNEGO authentication as described in [\[MS-AUTHSOD\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AUTHSOD%5d.pdf#Section_953d700a57cb4cf7b0c3a64f34581cc9) section 2.1.2.2. The client MUST also set the **Client.Connection.ServerGUID** to the value returned in the **ServerGUID** field in the SMB_COM_NEGOTIATE server response.[&lt;93&gt;](#Appendix_A_93)

#### Receiving an SMB_COM_SESSION_SETUP_ANDX Response

The processing of an [SMB_COM_SESSION_SETUP_ANDX response](#Section_e5a467bccd364afa825e3f2a7bfd6189) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.3 with the following additions:

**Extended Security Authentication**

If **Client.Connection.ServerCapabilities** has the CAP_EXTENDED_SECURITY bit set, then the client MUST reject any SMB_COM_SESSION_SETUP_ANDX responses that do not take the form specified in section 2.2.4.6.2. If the **Status** field of the SMB header is not STATUS_SUCCESS and is not STATUS_MORE_PROCESSING_REQUIRED, then the authentication has failed and the error code MUST be propagated back to the application that initiated the connection attempt. Otherwise, if there is no entry in **Client.Connection.SessionTable** for the UID in the response, then one MUST be created with the following additional requirements:

- **Client.Session.SessionUID** MUST be set to the UID in the response.
- **Client.Session.AuthenticationState** MUST be set to InProgress.
- **Client.Session.UserCredentials** MUST be set to the authentication credentials of the user that initiated the authentication attempt.
- **Client.Session.SessionKey** MUST be set to zero.
- **Client.Session.SessionKeyState** MUST be set to Unavailable.

The client MUST then process the GSS token (the **SecurityBlob** field of the response with its length given in the **SecurityBlobLength** field).[&lt;94&gt;](#Appendix_A_94) The client MUST use the configured GSS authentication protocol to obtain the next GSS token for the authentication exchange. Based on the status code received in the response and the result from the GSS authentication protocol, one of the following actions MUST be taken:

- If the GSS authentication protocol indicates an error, then the error MUST be returned to the calling application that initiated the connection.
- If the **Status** field of the response contains STATUS_SUCCESS and the GSS authentication protocol does not indicate an error, then authentication is complete. The **Client.Session.AuthenticationState** MUST be set to Valid and the **Client.Session.SessionKey** MUST be set using the value queried from the GSS protocol. For information about how this is calculated for Kerberos authentication via Generic Security Service Application Programming Interface (GSS-API), see [\[MS-KILE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9). The client MUST set **Client.Session.SessionKeyState** to Available.
- If the **Status** field of the response contains STATUS_MORE_PROCESSING_REQUIRED and the GSS authentication protocol did not indicate an error, then the client MUST create an [SMB_COM_SESSION_SETUP_ANDX request (section 2.2.4.6.1)](#Section_a00d03613544484596ab309b4bb7705d) with the following parameters:
  - The client MUST set CAP_EXTENDED_SECURITY in the **Capabilities** field and set SMB_FLAGS2_EXTENDED_SECURITY in the SMB header **Flags2** field.
  - The **SecurityBlob** and **SecurityBlobLength** fields MUST be set to the output token and its length returned by the GSS protocol.
  - The **SMB_Header.UID** field MUST be set to the value of the **SMB_Header.UID** field received in the SMB_COM_SESSION_SETUP_ANDX response.
  - This message MUST be sent to the server, and further processing listed in the remainder of this section is not necessary.

**NTLM Authentication**

If the CAP_EXTENDED_SECURITY bit in **Client.Connection.ServerCapabilities** is not set, then the client processes the response.[&lt;95&gt;](#Appendix_A_95) If the **Status** field of the response does not contain STATUS_SUCCESS, then the client MUST propagate the error to the application that initiated the authentication. The connection MUST remain open for the client to attempt another authentication.

If the **Status** field of the response contains STATUS_SUCCESS, then authentication was successful. The client associates the returned **SMB_Header.UID** of the response with this user for further requests, as specified in \[MS-CIFS\]. The **Client.Session.AuthenticationState** MUST be set to Valid. If the **Client.Session.SessionKey** is zero, then the client MUST query the authentication package for the 16-byte session key, as specified in [\[MS-NLMP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-NLMP%5d.pdf#Section_b38c36ed28044868a9ff8dd3182128e4), and set **Client.Session.SessionKey** to the returned value. If **Client.Session.SessionKey** is non-zero, then the client MUST NOT overwrite the existing session key. The client MUST set **Client.Session.SessionKeyState** to Available.

**Activating Signing**

If authentication has completed successfully, **Client.Connection.IsSigningActive** is FALSE, and the targeted behavior for this connection is signed according to the description in section [3.2.4.2.3](#Section_bfdbfa0c46f64982bce161762f29f3a7), then the client MUST determine whether signing is required to be activated.

To determine whether signing is required to be active, the user security context that completed authentication is verified. If the user that authenticated is a guest or is anonymous, then signing MUST NOT be activated. Guest authentication is indicated by bit zero in the **Action** field of the SMB_COM_SESSION_SETUP_ANDX response being set. Anonymous authentication is indicated by the fact that no credentials are provided.

If neither of these conditions are true, then the client MUST activate signing as follows:

- If CAP_EXTENDED_SECURITY is set in **Client.Connection.ServerCapabilities**, the client MUST use GSS-API to query the session key used in this authentication and store the ExportedSessionKey returned by GSS-API into **Client.Connection.SigningSessionKey**. The client MUST set **Client.Connection.SigningChallengeResponse** to NULL.
- If CAP_EXTENDED_SECURITY is not set in **Client.Connection.ServerCapabilities**, the client MUST use NTLM to query the session key used in this authentication.
  - For NTLMv1 - the client MUST store SessionBaseKey, returned by the **NTOWFv1** function defined in \[MS-NLMP\] section 3.3.1, into **Client.Connection.SigningSessionKey**.
  - For NTLMv2 - the client MUST store SessionBaseKey, returned by the **NTOWFv2** function defined in \[MS-NLMP\] section 3.3.2, into **Client.Connection.SigningSessionKey**.

The client MUST set **Client.Connection.SigningChallengeResponse** to the challenge response that is sent in the SMB_COM_SESSION_SETUP_ANDX response.

Once these steps are completed, the client MUST verify the signature of this response. The client follows the steps specified in section [3.1.5.1](#Section_2fa60c5a71ee4248a4e9dd5d0db2373d), by passing in a sequence number of one because this is the first signed packet.

#### Receiving an SMB_COM_TREE_CONNECT_ANDX Response

The processing of an [SMB_COM_TREE_CONNECT_ANDX Response](#Section_087860d5391941d5a7531b330d651196) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.4 with the following additions:

**Requesting Extended Information**

The client MUST determine whether or not the server returned an extended response, as specified in section [2.2.4.7](#Section_a5b03e8212aa4ebc93f348750299cdc9). The client does this by determining whether or not the **WordCount** is equal to 0x07. If it is, then the client MUST make the new extended information available to the calling application by using the **SMB_Header.TID** value to set **Client.Connection.TreeConnectTable\[TID\].MaximalShareAccessRights** and **Client.Connection.TreeConnectTable\[TID\].GuestMaximalShareAccessRights** to the values that are in the response fields of **SMB_Parameters.Words.MaximalShareAccessRights** and **SMB_Parameters.Words.GuestMaximalShareAccessRights**, respectively.

**Session Key Protection**

If the response status is STATUS_SUCCESS and the SMB_EXTENDED_SIGNATURE bit is set in the **OptionalSupport** field of the SMB_COM_TREE_CONNECT_ANDX response, then the client MUST hash the session key of the calling user. This protects the key that is used for signing by making it unavailable to the calling applications.

The one-way hash MUST be performed on **Client.Session.SessionKey** that uses the HMAC-MD5 algorithm, as specified in [\[RFC2104\]](http://go.microsoft.com/fwlink/?LinkId=90314). The steps are as follows:

- Take the 16-byte user session key from **Client.Session.SessionKey**.
  - If this is an LM authentication where the session key is only 8 bytes, then zero extend it to 16 bytes.
  - If the session key is more than 16 bytes, then use only the first 16 bytes.
- Calculate the one-way hash as follows:
- CALL hmac_md5( SSKeyHash, 256, session key, session key length, digest )
- SET user session key = digest

The resulting 16-byte digest is treated as the user's new session key and is returned to the caller who requests it. SSKeyHash is the well-known constant array that is described in section [2.2.2.5](#Section_f9972df714c845638ffc6c8b228862ef).

After the session key has been hashed, the client MUST place the hash into **Client.Session.SessionKey** and set **Client.Session.SessionKeyState** to Available, which allows applications to query the session key.

If the TREE_CONNECT_ANDX_EXTENDED_SIGNATURE bit is not set, then the **Client.Session.SessionKey** is not changed and **Client.Session.SessionKeyState** MUST be set to Available.

#### Receiving an SMB_COM_NT_CREATE_ANDX Response

The processing of an SMB_COM_NT_CREATE_ANDX response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.36 with the following additions:

The client MUST determine whether the server returned an extended response, as specified in section [2.2.4.9.2](#Section_9e7d187492bd44098089ffc1a4a4d94e). It does this by checking for a proper **WordCount** value. If **WordCount** is not equal to 0x2A, then the client MUST process the response as specified in \[MS-CIFS\] section 3.2.5.36. Otherwise, the extended information that is specified in section 2.2.4.9.2 MUST also be propagated back to the calling application.

#### Receiving an SMB_COM_OPEN_ANDX Response

The processing of an SMB_COM_OPEN_ANDX response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.25 with the following additions:

The client MUST determine whether or not the server returned an extended response, as specified in section [2.2.4.1.2](#Section_2e946bbf5e0f4521b68398a7a4801c3c). It does this by checking whether or not the **WordCount** is equal to 0x13. If the response is not an extended response, then the client MUST process the response as specified in \[MS-CIFS\] section 3.2.5.25. If the response is an extended response, then the new information specified in section 2.2.4.1.2 MUST be propagated back to the calling application.

#### Receiving an SMB_COM_READ_ANDX Response

The processing of an SMB_COM_READ_ANDX response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.26 with the following additions:

The first two bytes of the **SMB_Parameters.Words.Reserved2\[\]** field, specified in \[MS-CIFS\] section 2.2.4.42.2, are interpreted as the 16-bit **DataLengthHigh** field (specified in section [2.2.4.2.2](#Section_54dd2a6b299c4c9b9f8871c5b0511f6e)). The remaining 8 bytes of **Reserved2\[\]** remain unused and MUST be ignored by the client. The **DataLengthHigh** field MUST contain the two most-significant bytes of a 32-bit length count of bytes read from the server.

#### Receiving an SMB_COM_WRITE_ANDX Response

The processing of an SMB_COM_WRITE_ANDX response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.27 with the following additions:

The 16-bit **SMB_Parameters.Words.Reserved** field, specified in \[MS-CIFS\] section 2.2.4.43.2, is now interpreted as a 16-bit **CountHigh** field followed by an 8-bit **Reserved** field. The **CountHigh** field MUST contain the two most-significant bytes of a 32-bit count of bytes written by the server.

#### Receiving any SMB_COM_NT_TRANSACT Response

The processing of any SMB_COM_NT_TRANSACT response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.40.

##### Receiving an NT_TRANSACT_IOCTL Response

The processing of an NT_TRANSACT_IOCTL response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.40.2 with the following additions.

###### Receiving an FSCTL_SRV_REQUEST_RESUME_KEY Function Code

If the response indicates that an error occurred, then the client MUST propagate the error to the application that initiated the call.

If the response indicates that the operation is successful, then the client MUST return the copychunk resume key that is received in the Data block of the response to the application that initiated the call.

###### Receiving an FSCTL_SRV_COPYCHUNK Function Code

The success or failure code MUST be returned to the calling application. The FSCTL_SRV_COPYCHUNK response (section [2.2.7.2.2](#Section_2564d6a8c1e24492bd7f7a1587c1becf)) MUST also be returned to the calling application in both success and failure situations.

##### Receiving an NT_TRANSACT_QUERY_QUOTA Response

If the response indicates an error occurred, then the client MUST propagate the error to the application that initiated the call.

If the response indicates the operation is successful, then the client MUST return the information received in the Data block of the response to the application that initiated the call.

##### Receiving an NT_TRANSACT_SET_QUOTA Response

The client MUST propagate the success or failure code in the response to the application that initiated the call.

#### Receiving an SMB_COM_SEARCH Response

The processing of an SMB_COM_SEARCH response is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.33.

#### Receiving any SMB_COM_TRANSACTION2 subcommand Response

Aside from the following subcommand responses, all other SMB_COM_TRANSACTION2 subcommand responses are handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.5.39.

##### Receiving any TRANS2_SET_FS_INFORMATION Response

The client MUST propagate the success or failure code in the response to the application that initiated the call.

### Timer Events

There are no new client timers other than those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.6.

### Other Local Events

There are no new client local events other than those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.2.7.

## Server Details

### Abstract Data Model

This section specifies a conceptual model of possible data organization that an implementation maintains in order to participate in this protocol. The described organization is provided to explain how the protocol behaves. This document does not mandate that implementations adhere to this model as long as their external behavior is consistent with what is described in this document.

The following elements extend the client abstract data model specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.1.

#### Global

**ServerStatistics**: Server statistical information. This contains all the members of the **STAT_SERVER_0** structure, as specified in [\[MS-SRVS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SRVS%5d.pdf#Section_accf23b00f57441c918543041f1b0ee9) section 2.2.4.39.

**Server.MessageSigningPolicy**: This ADM element is extended from the specification in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.1.1 to include a new possible value:

- Declined -- Message signing is disabled unless the other party requires it. If the other party requires message signing, then it MUST be used. Otherwise, message signing MUST NOT be used.

**Server.SupportsExtendedSecurity**: A flag that indicates whether or not this node supports Generic Security Services (GSS), as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378), for selecting the authentication protocol.

**Server.IsDfsCapable**: A Boolean that, if set, indicates that the server supports the Distributed File System.

**Server.MaxCopyChunks**: The maximum number of chunks the server will accept in a server-side data copy operation.

**Server.MaxCopyChunkSize**: The maximum number of bytes the server will accept in a single chunk for a server-side data copy operation.

**Server.MaxTotalCopyChunkSize**: The maximum total number of bytes the server will accept for a server-side data copy operation.

**Server.CopyChunkTimeOut**: The amount of time for which the server restricts the processing of a single server-side data copy operation.

#### Per Share

**Server.Share.ShareFlags** is a **DWORD** bitmask value that MUST contain zero or more of the values, as specified in [\[MS-SRVS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SRVS%5d.pdf#Section_accf23b00f57441c918543041f1b0ee9) section 2.2.2.5.

**Server.Share.IsDfs**: A Boolean that, if set, indicates that this share is configured for DFS. For more information, see [\[MSDFS\]](http://go.microsoft.com/fwlink/?LinkId=89945).

#### Per SMB Connection

**Server.Connection.GSSNegotiateToken:** A byte array that contains the token received during an extended security negotiation and that is remembered for authentication.

#### Per Pending SMB Command

There is no new state introduced per pending SMB command.

#### Per SMB Session

**Server.Session.AuthenticationState:** A session can be in one of four states:

- **InProgress**: A session setup (an extended SMB_COM_SESSION_SETUP_ANDX exchange, as described in section [3.2.4.2.4.1](#Section_495dd941077648aaaa8af1aa5eeadcea)) is in progress for this session for the first time.
- **Valid**: The session is valid and a session key and UID are available for this session.
- **Expired**: The Kerberos ticket for this session has expired and the session needs to be re-established.
- **ReauthInProgress**: A session setup (an extended SMB_COM_SESSION_SETUP_ANDX exchange, as described in section 3.2.4.2.4.1) is in progress for re-authentication of an expired or valid session.

**Server.Session.SessionKeyState:** The session key state. This can be either Unavailable or Available.

**Server.Session.AuthenticationExpirationTime**: A value that specifies the time at which the session will be expired.

#### Per Tree Connect

- **TreeConnect.MaximalAccess**: Access rights for the user that established the tree connect on **TreeConnect.Share**, in the format specified in section [2.2.1.4](#Section_6e848af95cb64e7383acb68698e3d920).

#### Per Unique Open

**Server.Open.GrantedAccess:** The access level granted on this Open.

### Timers

#### Authentication Expiration Timer

The Authentication Expiration Timer, a re-authentication timer, is used to mark an authentication as expired when its authentication-specific expiration time is reached. This timer controls the periodic scheduling of searching for sessions that have passed their Authentication expiration time. The server SHOULD[&lt;96&gt;](#Appendix_A_96) schedule this timer such that sessions are expired in a timely manner.

### Initialization

The Authentication Expiration Timer, as specified in section [3.3.2.1](#Section_d3866f1fcada48848b68999ee2142568), MUST be started at system startup. The following values MUST be initialized at system startup:

- **Server.MessageSigningPolicy** and **Server.SupportsExtendedSecurity** MUST be set based on system policy.[&lt;97&gt;](#Appendix_A_97) The value of this is not constrained by the values of any other policies.
- **Server.MaxCopyChunks** MUST be set to an implementation-specific[&lt;98&gt;](#Appendix_A_98) default value.
- **Server.MaxCopyChunkSize** MUST be set to an implementation-specific[&lt;99&gt;](#Appendix_A_99) default value.
- **Server.MaxTotalCopyChunkSize** MUST be set to an implementation-specific[&lt;100&gt;](#Appendix_A_100) default value.
- **Server.CopyChunkTimeOut** MUST be set to an implementation-specific[&lt;101&gt;](#Appendix_A_101) default value.

When an SMB connection is established, the following values MUST be initialized.

- **Server.Connection.GSSNegotiateToken** MUST be empty.

When an SMB session is established on an SMB connection, the following value MUST be initialized:

- **Server.Session.AuthenticationState** MUST be set to InProgress.
- **Server.Session.SessionKeyState** MUST be set to Unavailable.
- **Server.IsDFSCapable** MUST be set to FALSE.
- **Server.Share.IsDfs** MUST be set to FALSE.

All other values are initialized as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.3.

### Higher-Layer Triggered Events

#### Sending Any Message

This interface is used internally by the server to send a message to the client. It is not exposed to external callers.

No new global details are presented to a server that sends any message beyond what is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.4.1.

##### Sending Any Error Response Message

In response to an error in the processing of any SMB request, the server SHOULD[&lt;102&gt;](#Appendix_A_102) follow the format as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.4.1.2.

#### Server Application Queries a User Session Key

The application MUST provide:

- The security context of the user whose session is being sought.
- Either a valid ClientName or a valid **Open**.

The server MUST locate an SMB connection that uses either the application-supplied **ServerName** to look in the **Server.ConnectionTable\[ClientName\]** or the application-supplied **Open.Connection**. If a valid **Connection** is found, then the server MUST scan for an SMB session in the **Server.Connection.SessionTable** that matches the security context of the user. If no entry is found, then the application request MUST be failed with STATUS_INVALID_PARAMETER. If a **Session** is found but **Server.Session.SessionKeyState** is Unavailable, the request MUST be failed with STATUS_ACCESS_DENIED and **ServerStatistics.sts0_permerrors** MUST be increased by 1. If **Server.Session.SessionKeyState** is Available, then the first 16-bytes of **Server.Session.SessionKey** MUST be returned to the calling application.

#### DFS Server Notifies SMB Server That DFS Is Active

In response to this event, the SMB server MUST set the global state variable **Server.IsDfsCapable** to TRUE. If the DFS server is running on this computer, it MUST notify the SMB server that the DFS capability is available via this event.

#### DFS Server Notifies SMB Server That a Share Is a DFS Share

In response to this event, the SMB server MUST set the **Server.Share.IsDfs** to TRUE. When a DFS server running on this computer claims a share as a DFS share, it MUST notify the SMB server via this event.

#### DFS Server Notifies SMB Server That a Share Is Not a DFS Share

In response to this event, the SMB server MUST clear the Server.ShareIsDfs attribute of the share specified in section [3.3.1.2](#Section_04c81d07a5bb48b7a14f3e32ab5dcd27).

#### Server Application Updates a Share

The calling application MUST provide a share in the SHARE_INFO_503_I and SHARE_INFO_1005 structures as input parameters to update an existing share. The server MUST look up the share in **Server.ShareTable** through the tuple &lt;shi503_servername, shi503_netname&gt;. If the matching share is found, the server MUST update the share as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.4.10 with the following values set; otherwise, the server MUST return an implementation-dependent error.

- **Share.FileSecurity** MUST be set to shi503_security_descriptor.
- **Share.ShareFlags** MUST be set to shi1005_flags.

#### Server Application Requests Querying a Share

The calling application MUST provide the tuple &lt;ServerName, ShareName&gt; of the share that is being queried. The server MUST look up the share in the **Server.ShareTable**. If the matching share is found, the server MUST return a share in the SHARE_INFO_503_I and SHARE_INFO_1005 structures to the caller as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.4.12 with the following values set; otherwise, the server MUST return an implementation-dependent error.

| Output Parameters                           | MS-SMB Share Properties       |
| ------------------------------------------- | ----------------------------- |
| SHARE_INFO_503_I.shi503_security_descriptor | **Server.Share.FileSecurity** |
| SHARE_INFO_1005.shi1005_flags               | **Server.Share.ShareFlags**   |

### Message Processing Events and Sequencing Rules

#### Receiving Any Message

The following global details are presented to a server that receives any message in addition to what is specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.2.

**Signing**

If a message is received and **Server.Connection.IsSigningActive** is TRUE the server uses **Server.Connection.ServerNextReceiveSequenceNumber** and the signature MUST be verified, as specified in section [3.1.5.1](#Section_2fa60c5a71ee4248a4e9dd5d0db2373d).

The server MUST insert the sequence number for the response to this request into the **Server.Connection.ServerSendSequenceNumber** table with the **PID** and **MID** that identifies the request/response pair. (**PID** and **MID** are specified in \[MS-CIFS\] section 2.2.1.6.)

If the signature on the received packet is incorrect, then the server MUST return STATUS_ACCESS_DENIED (ERRDOS/ERRnoaccess) and **ServerStatistics.sts0_permerrors** MUST be increased by 1. If the signature on the current message is correct, then the server MUST take the following steps.

- IF request command EQUALS SMB_COM_NT_CANCEL THEN
- INCREMENT ServerNextReceiveSequenceNumber
- ELSE IF request has no response THEN
- INCREMENT ServerNextReceiveSequenceNumber BY 2
- ELSE
- SET ServerSendSequenceNumber\[PID,MID\] TO
- ServerNextReceiveSequenceNumber + 1
- INCREMENT ServerNextReceiveSequenceNumber BY 2
- END IF

**Session Validation and Re-authentication**

If the **SMB_Header.UID** of the request is zero, then the server does not need to check for the expiry because a session is not being used for this request.

If the **SMB_Header.UID** of the request is not zero, then the server MUST check the state of the session.

- If **Connection.SessionTable\[UID\].AuthenticationState** is equal to Expired or ReauthInProgress, and the received message is an SMB_COM_SESSION_SETUP_ANDX request (indicating a session renewal), the behavior is as specified in section [3.3.5.3](#Section_1f152df0a61d4e769af6da96fa783c02). For details on how the client handles a session expiration, see section [3.2.5.1](#Section_33cc843042ca4f05af296485c3560daf).
- If **Connection.SessionTable\[UID\].AuthenticationState** is equal to Expired or ReauthInProgress, and the received message is one of the following requests, the server MUST continue processing the request.
  - SMB_COM_CLOSE
  - SMB_COM_LOGOFF_ANDX
  - SMB_COM_FLUSH
  - SMB_COM_LOCKING_ANDX
  - SMB_COM_TREE_DISCONNECT

If the received message is not one of the preceding requests, the server SHOULD[&lt;103&gt;](#Appendix_A_103) fail all operations with STATUS_NETWORK_SESSION_EXPIRED until the session renewal is successful.

- If **Server.Connection.SessionTable** is not empty, **Server.Connection.SessionTable\[UID\].AuthenticationState** is InProgress, and the received message is not an SMB_COM_SESSION_SETUP_ANDX request, then the server SHOULD[&lt;104&gt;](#Appendix_A_104) fail all operations with STATUS_INVALID_HANDLE and MUST increase **ServerStatistics.sts0_permerrors** by 1.
- If **Server.Connection.SessionTable** is not empty, **SMB_Header.UID** is not found in **Server.Connection.SessionTable**, and the received message is not an SMB_COM_SESSION_SETUP_ANDX request, then the server MUST fail all operations with STATUS_SMB_BAD_UID and MUST increase **ServerStatistics.sts0_permerrors** by 1.
- If **Server.Connection.SessionTable** is empty, then the server SHOULD[&lt;105&gt;](#Appendix_A_105) disconnect the connection.
- If **Server.Connection.SessionTable\[UID\].AuthenticationState** is InProgress and the received message is an SMB_COM_SESSION_SETUP_ANDX request, the behavior is as specified in section 3.3.5.3.
- If **Server.Connection.SessionTable\[UID\].AuthenticationState** is Valid, then the server MUST allow all operations.

##### Scanning a Path for a Previous Version Token

If a request is a path-based operation (for example, SMB_COM_NT_CREATE_ANDX) and has SMB_FLAGS2_REPARSE_PATH set in the **Flag2** field of the SMB header, then the server MUST perform a parse of the path by checking for previous version tokens (section [2.2.1.1.1](#Section_bffc70f9b16a453b939a0b6d3c9263af)). If the flag is not set, then the server MAY[&lt;106&gt;](#Appendix_A_106) parse the path anyway.

If a previous version token is found in the pathname, but the file or directory does not exist for the given snapshot, then the server MUST fail the operation with STATUS_OBJECT_NAME_NOT_FOUND. If the file or directory does exist, then processing continues as normal, except that the execution is against the previous version selected.

If no previous version token is found in the pathname, the server MUST process the path-based operation normally.

##### Granting Oplocks

The server SHOULD grant oplocks according to the process specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.2.7, with the following additions:

- If **Server.Share.ShareFlags** contains the SHI1005_FLAGS_FORCE_LEVELII_OPLOCK bit as defined in [\[MS-SRVS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-SRVS%5d.pdf#Section_accf23b00f57441c918543041f1b0ee9) section 2.2.4.29, and the request is for NT_CREATE_REQUEST_OPLOCK or NT_CREATE_REQUEST_OPBATCH oplock, the server SHOULD[&lt;107&gt;](#Appendix_A_107) downgrade the request and grant a level II oplock.

#### Receiving an SMB_COM_NEGOTIATE Request

The processing of an [SMB_COM_NEGOTIATE request](#Section_7991af9adc99437cbaf8a3b4ca56b151) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.42, with the following additions:

**New Capabilities**

The new capabilities flags specified in section 2.2.4.5.1 MUST also be considered when setting the **SMB_Parameters.Words.Capabilities** field of the response based on the **Server.Capabilities** attribute.

**Generating Extended Security Token**

If the client indicated support for extended security by setting SMB_FLAGS2_EXTENDED_SECURITY in the Flags2 field of the SMB header of the SMB_COM_NEGOTIATE request, then the server SHOULD set CAP_EXTENDED_SECURITY in the SMB_COM_NEGOTIATE response if it supports extended security. The response MUST take the form specified in section [2.2.4.5.2](#Section_8f8ce04ae1844d17bfb22cf762e6f6c4).

The server SHOULD set the **SecurityBlob** of the SMB_COM_NEGOTIATE response to the first GSS token (or fragment thereof) produced by the GSS authentication protocol it is configured to use (GSS tokens are as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378)). Otherwise, it leaves it empty. This token is also stored in **Server.Connection.GSSNegotiateToken**.

The server MUST initialize its GSS mechanism with the Integrity, Confidentiality, and Delegate options and use the Server-Initiated variation, as specified in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461). The SMB_COM_NEGOTIATE response packet is sent to the client.[&lt;108&gt;](#Appendix_A_108)

#### Receiving an SMB_COM_SESSION_SETUP_ANDX Request

The processing of an [SMB_COM_SESSION_SETUP_ANDX request](#Section_a00d03613544484596ab309b4bb7705d) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.43 with the following additions:[&lt;109&gt;](#Appendix_A_109)

**Storing Client Capabilities**

If **Server.Connection.ClientCapabilities** is equal to zero, then the server MUST set **Server.Connection.ClientCapabilities** to the **Capabilities** field that is received in the SMB_COM_SESSION_SETUP_ANDX request. If **Server.Connection.ClientCapabilities** has already been determined and is nonzero, then the server MUST ignore the capabilities value on subsequent requests.

**Determine Reauth or Continuation of Previous Auth**

If the **SMB_Header.UID** is not zero, the server MUST obtain the user name:

- If **Server.Connection.SessionTable\[UID\].UserSecurityContext** is NULL, the server MUST set it to a value representing the user that successfully authenticated this connection. The **UserSecurityContext** MUST be obtained from the GSS authentication subsystem. If it is not NULL, no changes are necessary.
- The server MUST invoke the GSS_Inquire_context call as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378) section 2.2.6, passing **Server.Connection.SessionTable\[UID\].UserSecurityContext** as the input parameter, and obtain the user name returned in "src \_name".

If the received user name is not equal to **Server.Connection.SessionTable\[UID\].UserName**, the server MAY fail the session setup and tear down the underlying transport connection.

Otherwise, the server MUST look up the authentication state for this session and take the following actions based on this state.

- If **Server.Connection.SessionTable\[UID\].AuthenticationState** is InProgress or ReAuthInProgress, then this is a continuation of an authentication in progress. This state indicates that the authentication required multiple roundtrips, and that authentication continues.
- If **Server.Connection.SessionTable\[UID\].AuthenticationState** is Valid or Expired, then this is the re-authentication of a user. The server MUST set AuthenticationState to ReAuthInProgress and begin a new authentication for this session. The server MUST prevent any further operations from executing on this session until authentication is complete, and fail them with STATUS_NETWORK_SESSION_EXPIRED.
- If there is no session for the provided UID, then the request MUST be failed with STATUS_SMB_BAD_UID.

**Extended Security**

If CAP_EXTENDED_SECURITY is set in **Server.Connection.ClientCapabilities**, then the server MUST handle the authentication as defined in this section. Otherwise, it MUST continue to the following NTLM authentication section.

The server MUST extract the GSS token, which is the **SecurityBlob** contained in the request, with a length of **SecurityBlobLength**.[&lt;110&gt;](#Appendix_A_110) The server MUST use the configured GSS authentication protocol to obtain the next GSS output token for the authentication protocol exchange. Note that this token can be 0 bytes in length.

If the GSS mechanism indicates an error that is not STATUS_MORE_PROCESSING_REQUIRED, then the server MUST fail the client request, and return only an SMB header and propagate the failure code. If a **UID** was present in this request, then its associated session MUST be removed from the **Server.Connection.SessionTable**. The authentication has failed and no further processing is done on this request. This error response is sent to the client.

If the GSS mechanism indicates success, then the server MUST create an [SMB_COM_SESSION_SETUP_ANDX response (section 2.2.4.6.2)](#Section_e5a467bccd364afa825e3f2a7bfd6189). The **SecurityBlob** MUST be set to the output token from the GSS mechanism, and **SecurityBlobLength** is set to the length of the output token. SMB_FLAGS2_EXTENDED_SECURITY is set in the **Flags2** field of the SMB header of the response. If the request did not specify a **UID** in the SMB header of the request, then a **UID** MUST be generated to represent this user's authentication and its value MUST be placed in the **UID** field of the SMB header of the response.

If the GSS mechanism indicates that the current output token is the last output token of the authentication exchange based on the return code, as specified in \[RFC2743\], the **Status** field in the SMB header of the response MUST be set to STATUS_SUCCESS, and **Server.Connection.SessionTable\[UID\].AuthenticationState** MUST be set to Valid. If the client sets the CAP_DYNAMIC_REAUTH capability in the request or the Kerberos authentication protocol enforces session re-authentication, **Server.Session.AuthenticationExpirationTime** SHOULD[&lt;111&gt;](#Appendix_A_111) be set to the authentication (either NTLM or GSS processing) expiration time returned by the GSS authentication protocol, such as a Kerberos ticket time-out. If this is not the case, **Server.Session.AuthenticationExpirationTime** SHOULD be set to infinity.

Otherwise, the **Status** field in the SMB header of the response MUST be set to STATUS_MORE_PROCESSING_REQUIRED, and **Server.Connection.SessionTable\[UID\].AuthenticationState** MUST be set to InProgress.

**Activating Signing**

If **Server.Connection.IsSigningActive** is FALSE, and the response of the SMB_COM_SESSION_SETUP_ANDX operation contains STATUS_SUCCESS, then the server MUST determine whether or not signing can be activated.

If bit zero of the **Action** field of the SMB_COM_SESSION_SETUP_ANDX response is set, then signing MUST NOT be activated. If the value of this field is one, then the user attempted to log in as a user other than Guest, but could not be authenticated for that account. Using a fallback mechanism on the server, the user is now logged in as Guest.

Otherwise, **Server.Connection.IsSigningActive** MUST be set to TRUE if any of the following conditions are satisfied:

- **Server.MessageSigningPolicy** is Required.
- The SMB_FLAGS2_SMB_SECURITY_SIGNATURE_REQUIRED bit in the **Flags2** field of the SMB header of the request is set.
- **Server.MessageSigningPolicy** is Enabled and the SMB_FLAGS2_SMB_SECURITY_SIGNATURE bit in the **Flags2** field of the SMB header of the request is set.

The server MUST query the authentication protocol, either using NTLM or via GSS API, for the session key used in this authentication, and store it as **Server.Connection.SigningSessionKey**. If CAP_EXTENDED_SECURITY is set in **Server.Connection.ClientCapabilities**, then it MUST set **Server.Connection.SigningChallengeResponse** to NULL. If that capability is not set, then it MUST set **Server.Connection.SigningChallengeResponse** to the challenge response received in the SMB_COM_SESSION_SETUP_ANDX request.

Once these steps are performed, the server MUST sign the SMB_COM_SESSION_SETUP_ANDX response. The server follows the steps as specified in section [3.1.5](#Section_a07570c4cf07416597c8205fbf1b0fb0) by passing in a sequence number of one.

**Acquire Session Key**

If authentication is successful, the server MUST query the session key from the authentication package (as specified in [\[MS-NLMP\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-NLMP%5d.pdf#Section_b38c36ed28044868a9ff8dd3182128e4) for implicit NTLM and in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461) for extended security). If the session key is equal to or longer than 16 bytes, the session key MUST be stored in **Server.Session.SessionKey**. Otherwise, the session key MUST be stored in **Server.Session.SessionKey** and MUST be padded with zeros up to 16 bytes. The server MUST set **Server.Session.SessionKeyState** to Unavailable.

**Authentication Expiry**

If **Server.Session.AuthenticationExpirationTime** expires, the Authentication Expiration Timer marks the **Server.Connection.SessionTable\[UID\].AuthenticationState** as Expired when the time-out occurs, as specified in [3.3.2.1](#Section_d3866f1fcada48848b68999ee2142568).

#### Receiving an SMB_COM_TREE_CONNECT_ANDX Request

The processing of an [SMB_COM_TREE_CONNECT_ANDX request](#Section_16b173568eff49c29d21557e07ef085d) is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.45 with the following additions:[&lt;112&gt;](#Appendix_A_112)

**Requesting Extended Information**

If the TREE_CONNECT_ANDX_EXTENDED_RESPONSE is set in the **Flags** field of the SMB_COM_TREE_CONNECT_ANDX request, then the server MUST respond with the structure specified in section [2.2.4.7.2](#Section_087860d5391941d5a7531b330d651196).

The server MUST populate the **SMB_Parameters.Words.OptionalSupport** field of the response with a value of **Server.Share.OptionalSupport**.

The server SHOULD[&lt;113&gt;](#Appendix_A_113) set SMB_UNIQUE_FILE_NAME bit in the **OptionalSupport** field if **Share.ShareFlags** contains the SHI1005_FLAGS_ALLOW_NAMESPACE_CACHING constant.

The server MUST calculate the maximal share access rights for the user that requests the tree connect using the following algorithm.

- MaxRights = 0x00000000
- IF Server.Share.FileSecurity == NULL
- MaxRights = 0xFFFFFFFF
- ELSE
- FOR EACH AccessBit value defined in section 2.2.1.4
- Compute access for the user, using Server.Share.FileSecurity and
- Server.Session.SecurityContext, as described in \[MS-DTYP\] section 2.5.2.1.
- IF access was granted
- MaxRights = MaxRights | AccessBit;
- END IF
- END FOR
- END IF

The computed MaxRights ACCESS_MASK MUST be placed in the **SMB_Parameters.Words.MaximalShareAccessRights** of the response. The server MUST set **TreeConnect.MaximalAccess** to **MaximalShareAccessRights**. If no access is granted for the client on this share, the server MUST fail the request with STATUS_ACCESS_DENIED and MUST increase **ServerStatistics.sts0_permerrors** by 1.

Using the same algorithm, the **SMB_Parameters.Words.GuestMaximalAccessRights** field of the response SHOULD[&lt;114&gt;](#Appendix_A_114) be set to the calculated highest access rights the guest account has on this share. Instead of using **Server.Session.SecurityContext**, the server MUST use the guest account's security context. If the system does not support the guest account, then it MUST set **GuestMaximalAccessRights** to zero.

**Session Key Protection**

If the client has set the TREE_CONNECT_ANDX_EXTENDED_SIGNATURE bit in the **Flags** field of the SMB_COM_TREE_CONNECT_ANDX request, then the server MUST hash the session key of the calling user. This protects the key used for signing by making it unavailable to server-side applications.

The one-way hash MUST be performed on the user session key by using the HMAC-MD5 algorithm, as specified in [\[RFC2104\]](http://go.microsoft.com/fwlink/?LinkId=90314). The steps are as follows:

- Take the 16-byte user session key from **Server.Session.SessionKey**.
  - If this is an LM authentication where the session key is only 8 bytes, then zero extend it to 16 bytes.
  - If the session key is more than 16 bytes, then use only the first 16 bytes.
- Calculate the one-way hash as follows:
- CALL hmac_md5( SSKeyHash, 256, session key, session key length, digest )
- SET user session key = digest

The resulting 16-byte digest is treated as the user's new session key and returned to the caller who requests it. SSKeyHash is the well-known constant array that is described in section [2.2.2.5](#Section_f9972df714c845638ffc6c8b228862ef).

After the session key has been hashed, the server MUST place the hash into **Server.Session.SessionKey** and set **Server.Session.SessionKeyState** to Available, which allows applications to query the session key. If the TREE_CONNECT_ANDX_EXTENDED_SIGNATURE bit is not set, then the **Server.Session.SessionKey** is not changed and **Server.Session.SessionKeyState** MUST be set to Available.

#### Receiving an SMB_COM_NT_CREATE_ANDX Request

The processing of an SMB_COM_NT_CREATE_ANDX request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.51 with the following additions:

The **ImpersonationLevel** in the request MUST have one of the values specified in section [2.2.4.9.1](#Section_8e14ed93f27544d1bc46dfaf296c91b1). Otherwise, the server MUST fail the request with STATUS_BAD_IMPERSONATION_LEVEL.

When opening a named pipe, if the **ImpersonationLevel** level is SECURITY_DELEGATION, the server MUST fail the request with STATUS_BAD_IMPERSONATION_LEVEL.

If during the open processing the underlying object store returns STATUS_ACCESS_DENIED as specified in [\[MS-FSA\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSA%5d.pdf#Section_860b1516c45247b4bdbc625d344e2041) section 2.1.5.1, Server Requests an Open of a File, the server MUST fail the request with STATUS_ACCESS_DENIED and MUST increase **ServerStatistics.sts0_permerrors** by 1.

If the underlying object store determines that encryption processing is required as specified in \[MS-FSA\] section 2.1.5.1 Server Requests an Open of a File, the object store MUST return STATUS_CS_ENCRYPTION_EXISTING_ENCRYPTED_FILE if the encrypted file exists or STATUS_CS_ENCRYPTION_NEW_ENCRYPTED_FILE if the file to be created will be encrypted, indicating that a UserCertificate is necessary to successfully complete the operation. In these cases, the server SHOULD attempt to obtain a user certificate by invoking the **Application Requests for a User-Certificate Binding** as specified in [\[MS-EFSR\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-EFSR%5d.pdf#Section_08796ba801c8487292211000ec2eff31) section 3.1.4.1, passing the **Server.Session.SecurityContext** as the security context of the user. If the enrollment fails, the server MUST fail the request with the resulting error. Otherwise, the server SHOULD repeat the handling as specified in \[MS-CIFS\] section 3.3.5.51, extended[&lt;115&gt;](#Appendix_A_115) to additionally pass the returned certificate to the object store as the **UserCertificate** argument.

If FILE_DELETE_ON_CLOSE is set in the **CreateOptions** field and any of the following conditions is TRUE, the server SHOULD[&lt;116&gt;](#Appendix_A_116) fail the request with STATUS_ACCESS_DENIED.

- **DesiredAccess** does not include DELETE or GENERIC_ALL.
- **Treeconnect.MaximalAccess** does not include DELETE or GENERIC_ALL.

The server MUST ignore all **CreateOptions** on a named pipe except FILE_WRITE_THROUGH, FILE_SYNCHRONOUS_IO_ALERT, and FILE_SYNCHRONOUS_IO_NONALERT.

For a named pipe request, if the client sets FILE_SYNCHRONOUS_IO_ALERT or FILE_SYNCHRONOUS_IO_NONALERT bits in the **CreateOptions** field and does not set the SYNCHRONIZE bit in the **DesiredAccess** field, the server MUST fail the **Open** request with STATUS_INVALID_PARAMETER.

On a successful create or open, if the NT_CREATE_REQUEST_EXTENDED_RESPONSE flag was set in the **Flags** field of the request, the server SHOULD[&lt;117&gt;](#Appendix_A_117) send an extended response (section [2.2.4.9.2](#Section_9e7d187492bd44098089ffc1a4a4d94e)).

If the server sends the new response, it MUST construct a response as specified in section 2.2.4.9.2, with the addition of the following rules:

- The server MUST query the underlying object store for file attributes and SHOULD[&lt;118&gt;](#Appendix_A_118) set the **FileStatusFlags** in the response, in an implementation-specific manner.
- If the underlying object store of the share in which the file is opened or created does not support streams, then the server MUST set the NO_SUBSTREAMS bit in the **FileStatusFlags** field.[&lt;119&gt;](#Appendix_A_119)
- The server SHOULD[&lt;120&gt;](#Appendix_A_120) set the **VolumeGUID** and **FileId** fields to zero.
- The server MUST query the underlying object store for the granted access rights on the returned **Server.Open**. The server MUST use the granted access rights and SHOULD[&lt;121&gt;](#Appendix_A_121) set the **MaximalAccessRights** and **GuestMaximalAccessRights** fields in an implementation-specific manner. If the file has no security applied, **MaximalAccessRights** MUST be set to 0xFFFFFFFF. The server MUST use **Open.Session.UserSecurityContext** to impersonate the client.

If **Server.IsDfsCapable** is TRUE and **Server.Share.IsDfs** is True, then server MUST invoke the interface defined in [\[MS-DFSC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-DFSC%5d.pdf#Section_3109f4be2dbb42c99b8e0b34f7a2135e) section 3.2.4.1 to normalize the pathname by supplying **FileName** as the input parameter. If normalization fails, the server MUST fail the create request with the error code returned by the DFS normalization routine. If the normalization procedure succeeds, returning an altered target name, the **FileName** field MUST be set to the normalized path name, and used for further operations specified in section in \[MS-CIFS\] section 3.3.5.51.

#### Receiving an SMB_COM_OPEN_ANDX Request

The processing of an SMB_COM_OPEN_ANDX request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.35 with the following additions:

If during the open processing the underlying object store returns STATUS_ACCESS_DENIED as specified in [\[MS-FSA\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSA%5d.pdf#Section_860b1516c45247b4bdbc625d344e2041) section 2.1.5.1.2, Server Requests an Open of an Existing File, the server MUST fail the request with STATUS_ACCESS_DENIED and MUST increase **ServerStatistics.sts0_permerrors** by 1.

If the underlying object store determines that encryption processing is required as specified in \[MS-FSA\] section 2.1.5.1.2 Open of an Existing File, the object store MUST return STATUS_CS_ENCRYPTION_EXISTING_ENCRYPTED_FILE, indicating that a UserCertificate is necessary to successfully complete the operation. In this case, the server SHOULD attempt to obtain a user certificate by invoking the **Application Requests for a User-Certificate Binding** as specified in [\[MS-EFSR\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-EFSR%5d.pdf#Section_08796ba801c8487292211000ec2eff31) section 3.1.4.1, passing the **Server.Session.SecurityContext** as the security context of the user. If the enrollment fails, the server MUST fail the request with the resulting error. Otherwise, the server SHOULD repeat the handling as specified in \[MS-CIFS\] section 3.3.5.35, extended [&lt;122&gt;](#Appendix_A_122) to additionally pass the returned certificate to the object store as the **UserCertificate** argument.

On a successful open, if the SMB_OPEN_EXTENDED_RESPONSE flag was set in the **Flags** field of the request, then the server SHOULD send an extended response, as specified in section [2.2.4.1.2](#Section_2e946bbf5e0f4521b68398a7a4801c3c).

If the server chooses to send the new response, then it MUST construct a response as detailed in section 2.2.4.1.2. The server MUST query the underlying object store for the granted access rights on the returned **Server.Open**. The server MUST use the granted access rights and SHOULD[&lt;123&gt;](#Appendix_A_123) set the **MaximalAccessRights** and **GuestMaximalAccessRights** fields in an implementation-specific manner. If the file has no security applied, **MaximalAccessRights** MUST be set to 0xFFFFFFFF. If no access is granted for the client on this share, the server MUST fail the request with STATUS_ACCESS_DENIED and MUST increase **ServerStatistics.sts0_permerrors** by 1.

#### Receiving an SMB_COM_READ_ANDX Request

The processing of an SMB_COM_READ_ANDX request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.36 with the following additions:[&lt;124&gt;](#Appendix_A_124)

If the read operation is on a named pipe, then the **Timeout_or_MaxCountHigh** field MUST be interpreted as the 32-bit **Timeout** field, as specified in section [2.2.4.2.1](#Section_df9244e87b2d4714a3836990589a8ff4).

If the read operation is on a file, then the **Timeout_or_MaxCountHigh** field MUST be interpreted as the 16-bit **MaxCountHigh** field followed by a 16-bit **Reserved** field, as specified in section 2.2.4.2.1. The value in **MaxCountHigh** MUST be treated as the two most significant bytes of the count of bytes to read and is combined with the value of **MaxCountOfBytesToReturn** to create a 32-bit count of bytes to read (as specified in section [3.2.4.4](#Section_ce5b3e8052904fde92e71f1f846adacb)). If **MaxCountHigh** is set to 0xFFFF, then the value MUST be ignored, and only the length received in **MaxCountOfBytesToReturn** is used.

It is acceptable to return fewer bytes than requested by the client, with the restriction that reads from named pipes or devices MUST return at least **MinCountOfBytesToReturn** bytes. If the read operation is on a file and the count of bytes to read is greater than or equal to 0x00010000 (64K), then the server MAY[&lt;125&gt;](#Appendix_A_125)Return the requested number of bytes in the response, set the two least significant bytes of the count in the **DataLength** field in the response, and the two most significant bytes of the count in the **DataLengthHigh** field (specified in section [2.2.4.2.2](#Section_54dd2a6b299c4c9b9f8871c5b0511f6e)).

#### Receiving an SMB_COM_WRITE_ANDX Request

The processing of an SMB_COM_WRITE_ANDX request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.37 with the following additions:

If CAP_LARGE_WRITEX is set in **Server.Connection.ClientCapabilities**, then it is possible that the count of bytes to be written is larger than the server's **MaxBufferSize**. The count of bytes to be written is specified in the **DataLength** and **DataLengthHigh** fields sent in the request, as specified in section [2.2.4.3.1](#Section_178be656705649ea8bcbcf123737b016). If the size of **SMB_Data.Bytes.Data** is not equal to (**DataLength** | **DataLengthHigh** <<16), the server SHOULD[&lt;126&gt;](#Appendix_A_126) fail the request and return ERRSRV/ERRerror.

If the server successfully writes data to the underlying object store, then the count of bytes written MUST be set in the **Count** and **CountHigh** fields of the response, as specified in section [2.2.4.3.2](#Section_056d7d3304574f9ab7e0ab983ce24ae4).

#### Receiving an SMB_COM_SEARCH Request

The processing of an SMB_COM_SEARCH request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.47, with the following additions:

If the **FileName** field in the request is an empty string, the server SHOULD[&lt;127&gt;](#Appendix_A_127) return the root directory information in the response.

#### Receiving any SMB_COM_TRANSACTION2 subcommand

The processing of any SMB_COM_TRANSACTION2 subcommand request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.58 with the following additions:

##### Receiving any Information Level

If the server receives client request with a pass-through Information Level (section [2.2.2.3.5](#Section_ab2aca376c9e4505baa99e2bc556c475)) and the server supports the CAP_INFOLEVEL_PASSTHRU capability in **Server.Capabilities**, then the server MUST decrement the Information Level value by SMB_INFO_PASSTHROUGH by treating the value as little-endian, and pass that value to the underlying object store. If the Information Level includes any request data, then the data MUST also be passed to the underlying object store.[&lt;128&gt;](#Appendix_A_128)

If the server does not support pass-through Information Levels, then it MUST fail this request with STATUS_INVALID_PARAMETER.

The returned status and response data, if any, are sent to the client in a Trans2 subcommand response message that corresponds to the same subcommand that initiated the request.

##### Receiving a TRANS2_FIND_FIRST2 Request

**New Information Levels**

The server SHOULD allow for the new Information Levels, as specified in section [2.2.2.3.1](#Section_34b83ded0f52406f887a83fe89f33e23). If the server does not support the new Information Levels, then it MUST fail the operation with STATUS_NOT_SUPPORTED.[&lt;129&gt;](#Appendix_A_129)

**Enumerating Previous Versions**

If a scan for previous version tokens (section [3.3.5.1.1](#Section_c2e66005cfe54ddbb5064887ee52c1c5)) reveals that the **FileName** of the TRANS_FIND_FIRST2 request contains the search pattern @GMT-\* and the requested Information Level is SMB_FIND_FILE_BOTH_DIRECTORY_INFO, then the server MAY[&lt;130&gt;](#Appendix_A_130) choose to return an enumeration of previous versions that are valid for the share. It does this by manufacturing a file entry for each previous version, as defined in section [2.2.8.1.1](#Section_03d05a6fbbaf4a9ea556036581b02737). If the server chooses not to do this, then the enumeration MUST be processed as a normal TRANS2_FIND_FIRST2 operation.

##### Receiving a TRANS2_FIND_NEXT2 Request

**New Information Levels**

If the query is started using one of the new Information Levels, as specified in section [2.2.2.3.1](#Section_34b83ded0f52406f887a83fe89f33e23), then the same Information Level structure MUST be used for the return of subsequent entries in the enumeration continuation.

**Enumerating Previous Versions**

Likewise, a query for previous version information that is started MUST be continued at the client's request with further entries generated, as defined in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).

##### Receiving a TRANS2_QUERY_FILE_INFORMATION Request

**Pass-through Information Levels**

If the client requests a pass-through Information Level, then the processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).

##### Receiving a TRANS2_QUERY_PATH_INFORMATION Request

**Pass-through Information Levels**

If the client requests a pass-through Information Level, then the processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).

##### Receiving a TRANS2_SET_FILE_INFORMATION Request

**Pass-through Information Levels**

If the client requests a pass-through Information Level, then the processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).[&lt;131&gt;](#Appendix_A_131)

##### Receiving a TRANS2_SET_PATH_INFORMATION Request

**Pass-through Information Levels**

If the client requests a pass-through Information Level, then the processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).[&lt;132&gt;](#Appendix_A_132)

##### Receiving a TRANS2_QUERY_FS_INFORMATION Request

**Pass-through Information Levels**

If the client requests a pass-through Information Level, then the processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).

##### Receiving a TRANS2_SET_FS_INFORMATION Request

The server MAY support setting file system information. If the server does not support setting file system information, then it MUST fail the request with STATUS_ACCESS_DENIED.

If the client requests a pass-through Information Level, then processing follows as specified in section [3.3.5.10.1](#Section_ed7e027124ee46eda08b3e1335b3c0ee).

There is no way to know if a server file system supports a given Information Level. From a protocol perspective, if a client issues a request and it fails with STATUS_NOT_SUPPORTED, then it MUST be inferred that the server file system does not support the request.

#### Receiving any SMB_COM_NT_TRANSACT Subcommand

The processing of any SMB_COM_NT_TRANSACT subcommand request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.59 with the following additions specified in section [3.3.5.11.1](#Section_7187919879ea439b970b8c301128edce).

##### Receiving an NT_TRANSACT_IOCTL Request

The NT_TRANSACT_IOCTL extensions listed in section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17) are not directly passed to the underlying object store. Instead, processing is as specified in the following sections.

If the IsFsctl field is set to zero, the server SHOULD[&lt;133&gt;](#Appendix_A_133) fail the request with STATUS_NOT_SUPPORTED.

When the server receives a pass-through FSCTL request, the server SHOULD[&lt;134&gt;](#Appendix_A_134) pass it through to the underlying object store.

When the server receives an undefined FSCTL or IOCTL operation request that does not meet the private FSCTL requirements of [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) 2.3, the server MUST NOT pass the request to the underlying object store and MUST fail the request with STATUS_NOT_SUPPORTED.

###### Receiving an FSCTL_SRV_ENUMERATE_SNAPSHOTS Function Code

This is a request to enumerate the available previous versions for a share. The server MUST return an enumeration of available previous versions, as specified in section [2.2.7.2.2](#Section_2564d6a8c1e24492bd7f7a1587c1becf). The **NumberOfSnapshots** MUST contain the total number of previous versions that are available for the volume and **NumberOfSnapshotsReturned** contains the number of entries that are returned in this enumeration. If **MaxDataCount** is not large enough to hold all of the entries, then the server SHOULD return zero entries. The value returned in **SnapShotArraySize** MUST be the size required to receive all of the available previous versions. If the **MaxDataCount** of the request is smaller than the size of an FSCTL_ENUMERATE_SNAPSHOTS response, then the server MUST fail the request with STATUS_INVALID_PARAMETER. When sending the response to the client, the server SHOULD NOT [&lt;135&gt;](#Appendix_A_135)include any additional data after NT_Trans_Data in the FSCTL_SRV_ENUMERATE_SNAPSHOTS response (as specified in section [2.2.7.2.2.1](#Section_5a43eb2950c846b68319e793a11f6226)) and the client MUST ignore any additional data on receipt.

If the server does not support this operation, then it SHOULD fail the request with STATUS_NOT_SUPPORTED.

###### Receiving an FSCTL_SRV_REQUEST_RESUME_KEY Function Code

This is a request for an opaque copychunk resume key for use in an FSCTL_SRV_COPYCHUNK operation. The server MUST generate a 24-byte value that is used to uniquely identify the open of the file against which this operation is executed.

If this operation is successful, then the server MUST construct an FSCTL_SRV_REQUEST_RESUME_KEY response as specified in section [2.2.7.2.2](#Section_2564d6a8c1e24492bd7f7a1587c1becf), with the following additional requirements:

The **CopychunkResumeKey** field MUST be the server-generated value.

If the generation of the Copychunk Resume Key fails, the server MUST fail the request with STATUS_INVALID_PARAMETER.

If the server does not support this operation, then it MUST fail the request with STATUS_NOT_SUPPORTED.

###### Receiving an FSCTL_SRV_COPYCHUNK Request

This is a request for a server-side data copy as specified in section [2.2.7.2.1](#Section_2f8a9baed8c1462893daadba011e4f17). The server MUST identify the source file based on the copychunk resume key field of the FSCTL_SRV_COPYCHUNK request. This copychunk resume key is a value that was returned by the server from an FSCTL_SRV_REQUEST_RESUME_KEY operation. If the copychunk resume key is not valid or does not represent an open file, then the server MUST fail the operation with STATUS_OBJECT_NAME_NOT_FOUND. If the file represented by the resume key is not opened for read-data access, then the server MUST fail the operation with STATUS_ACCESS_DENIED. Likewise, the target file MUST be specified by the Fid in the SMB_COM_NT_TRANSACTION request. If the target file is not opened for write-data access, then the server MUST fail the operation with STATUS_ACCESS_DENIED and **ServerStatistics.sts0_permerrors** MUST be increased by 1.

The server MUST validate that the amount of data to be written is within the server's configured bounds. If the server determines that the total chunk count is more than **Server.MaxCopyChunks**, or the size of any chunk is more than **Server.MaxCopyChunkSize**, or the total size of all chunks exceeds **Server.MaxTotalCopyChunkSize**, the server MUST fail the request with STATUS_INVALID_PARAMETER and return a response as specified in section [2.2.7.2.2](#Section_2564d6a8c1e24492bd7f7a1587c1becf).

The server MUST iterate through the data ranges specified in the request by reading data from the source offset of the source file and writing it to the target offset of the target file. If the underlying object store returns a failure, then the server MUST stop and set the response parameters, as specified in section 2.2.7.2.2, to indicate how much data was successfully written, and set the **Status** field of the header with the error code received.

If the processing of FSCTL_SRV_COPYCHUNK operation is completed before **Server.CopyChunkTimeOut**, the server MUST return the FSCTL_SRV_COPYCHUNK response as specified in section 2.2.7.2.2 with the following values and Status field of the header set to STATUS_SUCCESS:

- **ChunksWritten** is set to the number of copychunk operations requested by the client.
- **ChunkBytesWritten** is set to zero.
- **TotalBytesWritten** is set to the total number of bytes written to the destination file.

If the processing of FSCTL_SRV_COPYCHUNK operation is not completed before **Server.CopyChunkTimeOut**, the server MUST fail the call with **Status** field of the header set to STATUS_IO_TIMEOUT and return the FSCTL_SRV_COPYCHUNK response as specified in section [2.2.7.2.2.2](#Section_c2571af45f264bfcba6738d26f16effc) with the following values:

- **ChunksWritten** is set to the number of copychunk operations performed by the server within the time specified by **Server.CopyChunkTimeOut**.
- **ChunkBytesWritten** is set to zero.
- **TotalBytesWritten** is set to the total number of bytes written to the destination file within the time specified by **Server.CopyChunkTimeOut**.

If the server does not support this operation, then it MUST fail the request with STATUS_NOT_SUPPORTED.

##### Receiving an NT_TRANS_QUERY_QUOTA Request

The server MUST query the underlying object store, in an implementation-specific manner, to enumerate the quota information for the list of SIDs specified in the **SidList** field, on which the file or directory indicated by the **Server.Open** identified by the **SMB_Parameters.Words.Setup.FID** field of the request resides.[&lt;136&gt;](#Appendix_A_136) If the underlying object store does not support quotas, then the server MUST return STATUS_NOT_SUPPORTED.

The server MUST return as much of the available quota information that is able to fit in the maximum response buffer size denoted by **MaxDataCount**. If the entire quota information cannot fit in the response buffer, then the server MUST return a status of STATUS_BUFFER_TOO_SMALL. Otherwise, the server MUST return STATUS_SUCCESS. The format of the request determines which entries need to be returned, as specified in section [2.2.7.5.1](#Section_9f3f73f99c4a42ba9f56e6352491d714). The server MUST place the quota information in the response, as specified in section [2.2.7.5.2](#Section_178d375d029c40aeb7b8bd01547fff51), and send the response back to the client.

##### Receiving an NT_TRANS_SET_QUOTA Request

The server MUST attempt to apply the provided quota information to the underlying object store on which the file or directory indicated by the Fid resides, in an implementation-specific manner.[&lt;137&gt;](#Appendix_A_137) If the underlying object store does not support quotas, then the server MUST return STATUS_NOT_SUPPORTED.

The server MUST apply the quota information provided in the **NT_Trans_Data** block of the request (see section [2.2.7.6.1](#Section_5172dc9ce7ad47fa86c0317b047a37eb)).

The resulting success or error received from the underlying object store MUST be returned in the response, as specified in section [2.2.7.6.2](#Section_84801befd12e4efba9310ed9a6e730ea).

##### Receiving an NT_TRANSACT_CREATE Request

The processing of this subcommand request is handled as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.5.59.1 with the following exception.

If the **MaxParameterCount** field of the SMB_COM_NT_TRANSACT request contains a value that is less than the size of the NT_TRANSACT_CREATE Response as specified in section [2.2.7.1.2](#Section_3527e3a938de444eaed28d300fbf0c09), the server SHOULD[&lt;138&gt;](#Appendix_A_138) fail the request with STATUS_INVALID_SMB (ERRSRV/ERRerror).

### Timer Events

#### Authentication Expiration Timer Event

When the Authentication Expiration Timer expires, the server MUST scan all sessions and it MUST set **Server.Connection.SessionTable\[UID\].AuthenticationState** to _Expired_, for which the **Server.Connection.SessionTable\[UID\].AuthenticationState** is valid and **Server.Session.AuthenticationExpirationTime** has passed, as specified in section [3.3.5.3](#Section_1f152df0a61d4e769af6da96fa783c02).

### Other Local Events

There are no new server local events other than those specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.3.7.

# Protocol Examples

The following sections describe common scenarios that indicate normal traffic flow on the wire in order to illustrate the extensions to CIFS that are specified in this document.

## Extended Security Authentication

The following diagram depicts the protocol message sequence for a multi-phase extended security exchange and previous versions enumeration and access on the share root folder.

![User authentication and session establishment sequence](data:image/png;base64,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)

Figure 4: User authentication and session establishment sequence

Descriptions of the fields in this example are specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) and section 2.2.4.1. Fields that are shown and highlighted in bold text are relevant to this extension. It is assumed that the client has successfully established a network connection with the server.

The client initiates the first message with an [SMB_COM_NEGOTIATE request](#Section_7991af9adc99437cbaf8a3b4ca56b151), as specified in \[MS-CIFS\]. The client specifies extended security negotiation in the header **Flags2** field. It also includes NT LM 0.12 in the dialect strings list. The server constructs an extended [SMB_COM_NEGOTIATE response](#Section_d883d0a55a0a46268e3e87b0b66b79aa) packet that is denoted by the _WordCount_ field. The server returns dialect index, its capabilities, GUID value, and the initial security binary large object (BLOB) obtained, as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378) and defined in the preceding figure.

FRAME 1. Client negotiate request

- Client -> Server: Command = SMB_COM_NEGOTIATE
- Flags2 Summary = 51207 (0xC807)
- 1100 1000 0000 0111
- .... 1... .... .... = Extended security negotiation is supported
- Dialect Strings
- PC NETWORK PROGRAM 1.0
- LANMAN1.0
- Windows for Workgroups 3.1a
- LM1.2X002
- LANMAN2.1
- NT LM 0.12

FRAME 2. Server negotiate response

- Server -> Client: Command = SMB_COM_NEGOTIATE
- NT status code = 0x0, STATUS_SUCCESS
- Word count = 17
- Protocol Index = 5 (NT LM 0.12)
- Capabilities = 2147607549 (0x8001F3FD)
- 1000 0000 0000 0001 1111 0011 1111 1101
- .... .... .... .... ..1. .... .... .... = Supports Pass-Thru levels
- 1... .... .... .... .... .... .... .... = Supports extended security
- Server GUID = 01 B3 1E 23 07 2A A4 4D A1 9F B6 69 F0 45 71 90
- Security Blob in payload

The client uses the initial security BLOB that is returned by the server along with any user credential information in order to obtain its security BLOB, as specified in \[RFC2743\] and defined in section [3.2.4.2.4](#Section_d3b7bcd3cd684d3b916b443ccd55f953). The resulting security BLOB is sent to the server as part of the [SMB_COM_SESSION_SETUP_ANDX extended request](#Section_a00d03613544484596ab309b4bb7705d). The client also sends its capabilities and zero **UID** to mark the start of a new session setup exchange. The server verifies that the client requests extended security by checking the **Flags2** and **Capabilities** fields in the request, accepts as input the client security BLOB, and processes it, as specified in \[RFC2743\]. In this case, the security package requires more processing and returns a second security BLOB to be returned to the client. Also, the server allocates a new UID and associates it with this session setup exchange.

**Note** Extended security can require multiple request and response exchanges between client and server to complete. The **UID** is defined by the server on first response to an extended session setup and is used for the lifetime of the session.

FRAME 3. Client request for extended session setup

- Client -> Server: Command = SMB_COM_SESSION_SETUP_ANDX
- Header: Tid = 0x0000 Mid = 0x0070 Uid = 0x0000
- Flags2 Summary = 51207 (0xC807)
- 1100 1000 0000 0111
- .... 1... .... .... = Supports extended security
- Word count = 12
- Capabilities = 0xA0000000
- 1010 0000 0000 0000 0000 0000 0000 0000
- ..1. .... .... .... .... .... .... .... = Supports dynamic reauth
- 1... .... .... .... .... .... .... .... = Requests extended security
- Security Blob Length = 74 (0x4A)
- Security Blob in payload

FRAME 4. Server response with session setup continuation

- Server -> Client: Command = SMB_COM_SESSION_SETUP_ANDX
- NT status code = 0xC0000016, STATUS_MORE_PROCESSING_REQUIRED
- Header: Tid = 0x0000 Mid = 0x0070 Uid = 0x0802
- Flags2 Summary = 51207 (0xC807)
- 1100 1000 0000 0111
- .... 1... .... .... = Extended security negotiation is supported
- Security Blob Length = 349 (0x15D)
- Security Blob in payload

The client accepts as input the server security BLOB and processes it, as specified in \[RFC2743\], and its output is returned to the server along with the UID. The server uses the **UID** value to associate this request with the pending session establishment. The server processes this request, as specified in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461), and receives a success result. At this point, the SMB_SESSION_SETUP_ANDX exchange is complete because the status code is not equal to STATUS_MORE_PROCESSING_REQUIRED. The final security BLOB is returned with the success indication.

FRAME 5. Client session setup request continuation

- Client -> Server: Command = SMB_COM_SESSION_SETUP_ANDX
- Header: Tid = 0x0000 Mid = 0x0080 Uid = 0x0802
- Flags2 Summary = 51207 (0xC807)
- 1100 1000 0000 0111
- .... 1... .... .... = Extended security negotiation is supported
- Word count = 12
- Security Blob Length = 226 (0xE2)
- Security Blob in payload

FRAME 6. Server response with session setup completion

- Server -> Client: Command = SMB_COM_SESSION_SETUP_ANDX
- NT status code = 0x0, STATUS_SUCCESS
- Header: Tid = 0x0000 Mid = 0x0080 Uid = 0x0802
- Security Blob Length = 9 (0x9)
- Security Blob in payload

At this point, the client has been successfully authenticated.

## Previous File Version Enumeration

The following example shows how the client accesses a previous version of the share root folder. It is assumed that the client has already authenticated, established a tree connect to the target share, and opened a handle to the root directory, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b). Thus, Frame 1 is not truly the first frame for the connection, but it is referred to as the starting point for this operation.

![Previous file version enumeration sequence](data:image/png;base64,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)

Figure 5: Previous file version enumeration sequence

The first step is to enumerate the list of available snapshots on the server by using the FSCTL_SRV_ENUMERATE_SNAPSHOT command. The client requests the list of snapshots that are available on the server by using the root handle Fid. The server returns the list of snapshots in the format that is defined in the preceding figure. In this example, the server has one snapshot total for the root folder, the payload contains one snapshot string, the payload size is 0x34 bytes, and the snapshot name is @GMT-2006.04.26-04-08-27. The last 2 bytes of the payload are the snapshot strings 16-bit Unicode NULL delimiter.

FRAME 1. Client requests FSCTL_SRV_ENUMERATE_SNAPSHOTS

- Client -> Server: Command = SMB_COM_NT_TRANSACT
- NT IOCTL Function Code 0x00144064 FSCTL_SRV_ENUMERATE_SNAPSHOTS
- File ID (Fid) = 16391 (0x4007)

FRAME 2. Server response with list of snapshots

- Server -> Client: Command = SMB_COM_NT_TRANSACT
- NT status code = 0x0, STATUS_SUCCESS
- Payload contained in Data buffer as defined in section 3.1.5.4:
- 00090: 01 00 00 00 01 00 00 00 34 00 00 00 40 00 ..........4...@.
- 000A0: 47 00 4D 00 54 00 2D 00 32 00 30 00 30 00 36 00 G.M.T.-.2.0.0.6.
- 000B0: 2E 00 30 00 34 00 2E 00 32 00 36 00 2D 00 30 00 ..0.4...2.6.-.0.
- 000C0: 34 00 2E 00 30 00 38 00 2E 00 32 00 37 00 00 00 4...0.8...2.7...
- 000D0: 00 00

The client uses standard SMB commands to access the snapshot. The client also indicates in the header **Flags2** that the name in the request is tokenized with the previous version information. This indicates to the server that the client is accessing a previous version of the path. The server processes the request and returns the path information for the snapshot directory rather than to the current directory.

FRAME 3. Client requests path information for snapshot 2006/04/26 04:08:27 AM

- Client -> Server: Command = SMB_COM_TRANSACTION2
- Flags2 Summary = 52231 (0xCC07)
- 1100 1100 0000 0111
- .... .1.. .... .... = File name is tokenized with Previous
- Version Information
- Transact2 function = Query path info
- File name =\\@GMT-2006.04.26-04.08.27
- 00080: 5C 00 40 00
- ……............\\.@.
- 00090: 47 00 4D 00 54 00 2D 00 32 00 30 00 30 00 36 00 G.M.T.-.2.0.0.6.
- 000A0: 2E 00 30 00 34 00 2E 00 32 00 36 00 2D 00 30 00 ..0.4...2.6.-.0.
- 000B0: 34 00 2E 00 30 00 38 00 2E 00 32 00 37 00 00 00 4...0.8...2.7...

FRAME 4. Server response with snapshot path information

- Server -> Client: Command = SMB_COM_TRANSACTION2
- NT status code = 0x0, STATUS_SUCCESS
- Data bytes = 40 (0x28)

Payload contains path information for specified snapshot version

Similar to its behavior during the query path exchange, the client specifies the previous version of the root folder in an open request. The server processes the request and returns an Fid for the specified previous version of the path.

FRAME 5. Client open request for version 2006/04/26 04:08:27 AM on "\\"

- Client -> Server: Command = SMB_COM_NT_CREATE_ANDX
- Flags2 Summary = 52231 (0xCC07)
- 1100 1100 0000 0111
- .... .1.. .... .... = File name is tokenized with Previous
- Version Information
- Create Disposition = Open: If exist, Open, else fail
- File name =\\@GMT-2006.04.26-04.08.27

FRAME 6. Server open root folder and returns Fid

- Server -> Client: Command = SMB_COM_NT_CREATE_ANDX
- NT status code = 0x0, STATUS_SUCCESS
- File ID (Fid) = 16392 (0x4008)
- Create Action = File Opened

These similar steps can be used to open a file rather than a directory on a remote volume. In that case, the @GMT token is contained in the relative path, such as \\directory\\@GMT-2006.04.26-04.08.27\\file.txt. This path can be used to query attributes or to open a file. The resulting Fid is used to read its contents.

Likewise, the @GMT token path in the example can be used as part of a TRANS2_FIND_FIRST2 and TRANS2_FIND_NEXT2 to enumerate the contents of the volume at the time of the snapshot.

## Message Signing Example

The following is the sequence of events that is related to SMB message authentication. In the following scenario, as specified in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461), authentication is used between the client and the server. The client and server are both configured not to require SMB signing; however, both are capable of using SMB signing. This also applies to the figure in section [4.1](#Section_ee3e4254083b423092893ca0f969ac5a); however, the parameters significant to signing a negotiation are called out.

- The client sends an [SMB_COM_NEGOTIATE request](#Section_7991af9adc99437cbaf8a3b4ca56b151) to the server.
- Client -> Server: SMB: C negotiate, Dialect = NTLM 0.12
- SMB Flags2 contains 0xC843
- 1... .... .... .... = Unicode Strings: Strings are Unicode
- .1.. .... .... .... = Error Code Type: Error codes are NT error codes
- ..0. .... .... .... = Execute-Only Reads: Do not permit reads if execute-only
- ...0 .... .... .... = Dfs: Do not resolve pathnames with Dfs
- .... 1... .... .... = Extended security negotiation is supported
- .... .... .1.. .... = Long Names Used
- .... .... .... .0.. = Security signatures are not supported
- .... .... .... ..1. = Extended Attributes: Extended attributes are supported
- .... .... .... ...1 = Long Names Allowed
- Security Signature is not set (the value is 00 00 00 00 00 00 00
- 00).
- SECURITY_SIGNATURE: Bit2 (not set)

No **SecuritySignature** is generated at this stage.

- The client receives an [SMB_COM_NEGOTIATE response](#Section_d883d0a55a0a46268e3e87b0b66b79aa) SMB from the server.
- Server -> Client: SMB: R negotiate, Dialect # = 5
- SMB Flags2 contains 0xC853
- Binary: 00000000 00000000 11001000 01010011
- ^ ^ ^
- SECURITY_SIGNATURE: Bit2: (not set)
- Security Signature is not set (the value is 00 00 00 00 00 00 00 00).

No **SecuritySignature** is generated at this stage.

- The client builds an [SMB_COM_SESSION_SETUP_ANDX request](#Section_a00d03613544484596ab309b4bb7705d) SMB and sends it to the server.

In the SessionSetupAndX SMB, an authentication request, such as an NTLM or NTLMv2 Challenge/Response or a Kerberos ticket, is sent from the client to the server.

At this stage, the SessionKey is not yet available.

- Client -> Server: SMB: C session setup & X
- SMB Flags2 contains 0xC807
- Binary: 00000000 00000000 11001000 00000111
- ^ ^ ^
- SECURITY_SIGNATURE: Bit2 (set)

After the packet is sent by the client, the sequence number is incremented to 1, which is the expected sequence number for the response packet from the server.

- The server processes the request and sends an [SMB_COM_SESSION_SETUP_ANDX response](#Section_e5a467bccd364afa825e3f2a7bfd6189) to the client.

It is possible that multiple roundtrips of SessionSetupAndX can be required to complete a given authentication. If STATUS_MORE_PROCESSING_REQUIRED is returned, then the implementer would return to the previous step and repeat. The following example demonstrates what happens when STATUS_SUCCESS is returned. Similarly, if this authentication was for Anonymous or Guest, then signing would not be activated at this time.

- Server -> Client: SMB: R session setup & X
- SMB Flags2 contains 0xC807
- Binary: 00000000 00000000 11001000 00000111
- ^ ^
- SECURITY_SIGNATURE: Bit2 (set)

The server sets the sequence number to 1 for the response packet and generates the **SecuritySignature** as follows.

The server places the sequence number (1) in the **SecuritySignature** field of the SMB header, and an MD5 hash is performed on the SessionKey + SMB packet. This results in a 16-byte value. The first 8 bytes of the computed hash (AB 44 C4 76 45 84 1A 6A) are placed in the **SecuritySignature** field and sent to the client.

- 00000: 00 11 43 02 26 E6 00 C0 4F 60 2E 45 08 00 45 00 ..C.&f.@O'.E..E.
- 00010: 01 78 85 60 40 00 80 32 F6 4B AC 1B 92 B9 AC 1B .x&'@.,2vK,.9,.
- 00020: 92 B7 88 F2 96 BD 00 00 00 14 01 BD 05 48 8B A1 "Fr=.....=.H9!
- 00030: 8F 6C C1 3F C0 39 50 18 FF F0 84 70 00 00 00 00 lA?@9P.pp....
- 00040: 01 2F FF 53 4D 42 73 00 00 00 00 98 07 C8 00 00 ./SMBs....\\.H..
- 00050: >AB 44 C4 76 45 84 1A 6A<00 00 00 00 FF FE 00 08 +DDvE.j....~..
- 00060: 40 00 04 FF 00 2F 01 00 00 A2 00 04 01 A1 81 9F @.../..."...!x
- 00070: 30 81 9C A0 03 0A 01 00 A1 0B 06 09 2A 86 48 82 0S
- ....!...\* H

After the server sends the packet, the sequence number is incremented to 2, which is the expected sequence number for the next SMB packet from the client.

- The client processes the response and obtains the SessionKey.
- SMB Flags2 contains 0xC807
- Binary: 00000000 00000000 11001000 00000111
- ^ ^
- SECURITY_SIGNATURE: Bit2 (set)

The expected sequence number is 1 for the response packet from the server.

The client saves the **SecuritySignature** in the response packet. The expected sequence number (1) is placed in the **SecuritySignature** field of the SMB header, and an MD5 hash is performed on the SessionKey SMB packet. This results in a 16-byte value. The first 8 bytes of the computed hash are compared with the one sent by the server (AB 44 C4 76 45 84 1A 6A) to validate the SMB packet. For the SessionKey that is used for signing when Kerberos is used, see [\[MS-KILE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9) section 3.1.1.2, Cryptographic Material.

- The client proceeds further and sends an [SMB_COM_TREE_CONNECT_ANDX request](#Section_16b173568eff49c29d21557e07ef085d) SMB.
- Client -> Server: SMB: C tree connect & X, Share

The client sequence number is now incremented. The new value is 2.

The sequence number (2) is placed in the **SecuritySignature** field of the SMB header, and an MD5 hash is performed on the 16-byte SessionKey + SMB packet. This results in a 16-byte value. The first 8 bytes (in this case, A5 B0 43 DC 07 51 0F 8B) are placed in the **SecuritySignature** field in the SMB header and then sent to the server.

- 00000: 00 C0 4F 60 2E 45 00 11 43 02 26 E6 08 00 45 00 .@O'.E..C.&f..E.
- 00010: 00 98 21 48 40 00 80 32 5B 44 AC 1B 92 B7 AC 1B .\\!H@.,2\[D,.",.
- 00020: 92 B9 C4 70 3D 34 00 00 00 1C 05 48 01 BD C1 3F 9Dp=4.....H.=A?
- 00030: C0 39 8B A1 90 9F 50 18 42 EF D0 D6 00 00 00 00 @99!xP.BoPV....
- 00040: 00 54 FF 53 4D 42 75 00 00 00 00 18 07 C8 00 00 .TSMBu......H..
- 00050: >A5 B0 43 DC 07 51 0F 8B<00 00 00 00 FF FE 00 08 %0C\\.Q.9....~..
- 00060: 80 00 04 FF 00 54 00 0C 00 01 00 29 00 00 5C 00 ,...T.....)..\\.
- 00070: 5C 00 4D 00 4F 00 48 00 41 00 4B 00 34 00 31 00 \\.M.O.H.A.K.4.1.

The sequence continues until the session is terminated.

In the case where extended security is not used, the same process is followed. However, the MD5 hash is performed on the 16-byte session key + NTLM challenge response + SMB packet with the appropriate sequence number. The NTLM challenge response is the authentication that is received in the SMB_COM_SESSION_SETUP_ANDX request in the **UnicodePassword** field if NTLM was used for authentication, or in the **OEMPassword** field if LM authentication was used.

## Copy File (Remote to Local)

The following example illustrates the sequence of operations during the copying of a file from a remote location to the local machine. The example assumes that the connection establishment and session management have already taken place.

![Copy file (remote to local) sequence](data:image/png;base64,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)

Figure 6: Copy file (remote to local) sequence

In the preceding diagram, the first frame is to open the remote file for read access. The subsequent frames read the data from the file, and then close the file. In between the read and the close, the data is written to the local file.

NT_CREATE_ANDX

- Client -> Server: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1712 (0x6B0)
- SMB: Command = C NT create & X
- SMB: Desired Access = 0x00000089
- SMB: ...............................1 = Read Data Allowed
- SMB: ..............................0. = Write Data Denied
- SMB: .............................0.. = Append Data Denied
- SMB: ............................1... = Read EA Allowed
- SMB: ...........................0.... = Write EA Denied
- SMB: ..........................0..... = File Execute Denied
- SMB: .........................0...... = File Delete Denied
- SMB: ........................1....... = File Read Attributes Allowed
- SMB: .......................0........ = File Write Attributes Denied
- SMB: NT File Attributes = 0x00000080
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................0..... = Not Archive
- SMB: .........................0...... = Not Device
- SMB: ........................1....... = Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. =
- CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File Share Access = 0x00000003
- SMB: ...............................1 = Read allowed
- SMB: ..............................1. = Write allowed
- SMB: .............................0.. = Delete not
- allowed
- SMB: Create Disposition = Open: If exist, Open, else fail
- SMB: Create Options = 68 (0x44)
- SMB: ...............................0 = non-directory
- SMB: ..............................0. = non-write through
- SMB: .............................1.. = Data is written to the file sequentially
- SMB: ............................0... = intermediate buffering allowed
- SMB: ...........................0.... = IO alerts bits not set
- SMB: ..........................0..... = IO non-alerts bit not set
- SMB: .........................1...... = Operation is on a non-directory file
- SMB: ........................0....... = tree connect bit not set
- SMB: .......................0........ = complete if oplocked bit is not set
- SMB: ......................0......... = no EA knowledge bit is not set
- SMB: .....................0.......... = 8.3 filenames bit is not set
- SMB: ....................0........... = random access bit is not set
- SMB: ...................0............ = delete on close bit is not set
- SMB: ..................0............. = open by filename
- SMB: .................0.............. = open for backup bit not set
- SMB: File name =\\filename.txt

NT_CREATE_ANDX Response

- Server -> Client: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1712 (0x6B0)
- SMB: Command = R NT create & X
- SMB: Oplock Level = Batch
- SMB: File ID (Fid) = 16389 (0x4005)
- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. = CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted

SMB_COM_READ_ANDX Request

- Client -> Server: SMB: C Read Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1744 (0x6D0)
- SMB: Command = C read & X
- SMB: File ID (Fid) = 16389 (0x4005)
- SMB: Max count = 1596 (0x63C)
- SMB: Min count = 1596 (0x63C)
- SMB: Bytes left = 1596

SMB_COM_READ_ANDX Response

- Server -> Client: SMB: R Read Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1744 (0x6D0)
- SMB: Command = C read & X
- SMB: Data length = 1596 (0x63C)
- SMB: Data offset = 60 (0x3C)
- SMB: Byte count = 1597
- Data = 00 90 27 D0 C4 6F 00 90 27 66 6D BE 08 00 45 00 ……

SMB_COM_CLOSE Request

- Client -> Server: SMB: C Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1984 (0x7C0)
- SMB: Command = C Close
- SMB: File ID (Fid) = 16389 (0x4005)

SMB_COM_CLOSE Response

- Server -> Client: SMB: R Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 1984 (0x7C0)

## Copy File (Local to Remote)

The following example illustrates the sequence of operations while copying a local file to a remote share. The frames do not include the connection establishment or session management, for example.

![Copy file (local to remote) sequence](data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAb0AAAFxCAYAAADnBHaLAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAAJcEhZcwAADFMAAAxhAdIjpQsAAC6ESURBVHhe7Z0veBRZ2kcjRiBWIJERCGQkIgKJWBGLGLFiBRKBQKyMGIFErkBgIxCIFUgkYgQCEbECGYGIWNPfd3ry63lzp/pPOnWT6uSc57lPV93/VUne0291CHtnZ2ezf/3rX5aRyvHx8ez8/HwmInJd3r59OxhnLNuV09PT2R4Hz549G+xguXo5ODiY/fvf/774lhUR2Y7Pnz/P9vf3B+OM5erl6Oho9o9//OMP6VFkHLipSk9ErgvSIyGRcSAuK70OKD0RGQOlNy5KrxNKT0TGQOmNi9LrhNITkTFQeuOi9Dqh9ERkDJTeuCi9Tig9ERkDpTcuSq8TSk9ExkDpjYvS64TSE5ExUHrjovQ6ofREZAyU3rgovU4oPREZA6U3LkqvE0pPRMZA6Y2L0uuE0hORMVB646L0OqH0RGQMlN64KL1OKD0RGQOlNy5KrxNKT0TGQOmNi9LrhNITkTFQeuOi9Dqh9ERkDJTeuCi9Tig9ERkDpTcuSq8TSk9ExkDpjYvS64TSE5ExUHrjMinp/fz5c3Z8fHxxttsoPREZA6U3LjcqvR8/fsyOjo5mjx8/XpTDw8PZu3fv5u1fv36d1wX6UsaCdbjYm0DpicgY9JIe8bDGYgrxluTjLnNj0kN43FQW4zi8f/9+IbZWet+/f5+XsSCLRLI3gdITkTHoIb2PHz/OYy1zB2ItcYs4fJe5MemxyLKsLe8sWunxBUkWGCIuCsKs0MYcrEU7r4E1WJ/56df7MSprKz0RuS49pLdpAkCCknhK/KySzMdRvBKnczwUW2mvCQyxO3G87U9f1qHQVtccgxuTHrJpBdbSSo8Lrl8Yjtks/bgR9K3i45w+rJObGvEpPRHZRYh1Y0uPGEksJONbRp7O0ZeYS0zlPE/qqNvb21vE2cRa+tS4nHky7vXr1/MxjKcQlzMWaKM/r/Stc43BjUqPC1wF7fQLVXqRWKWta9fgi1XbW4n2ROmJyBj0kB4gFGJmBEPMqlkV5/SpUJeEIfGahKJCe32qV88jwDomdYG9tOuOyY1Kb12aukp6HNPGzUuhrd5c2qv0OK6Sq/P1RumJyBj0kl4gTkZMZG7J/oiVQzGXvtDG69Bmdhwn9jOGNeqclDpPXaMHNyY9LmSdvdubWCW1ibAYyxyB4zpmkznGQumJyBj0ll6F+EjsyvEq+SyTHiAy4j17r31WjQnr1r0uNya9PA+uUgr1XUC9IVVSGd+m0vW8nZ/jKjmlJyK7Rg/pLfv9iiocYhjyaknMXSWwyI45qsCSBSbmD3FnpAcsxAXzDoCLioRyY1dJDzjODaG0XxTGrpJexJnxPWFvSk9ErksP6eWRYqREoa7GywiK+tonsXOV9IA2Sh5zBuI/9fFAzkNifC9uVHrAF5CL5MJ4RUSBdxD1YunbviOhf2RH33pDOa+ZH8dD4+sXrhdKT0TGoIf0gFhIHCQWExOXZX/U005Mq33aeN3CZ4PL5qweYA7OA2Pq+djcuPTuC0pPRMagl/TuK0qvE0pPRMZA6Y2L0uuE0hORMVB646L0OqH0RGQMlN64KL1OKD0RGQOlNy5KrxNKT0TGQOmNi9LrhNITkTFQeuOi9Dqh9ERkDJTeuCi9Tig9ERkDpTcuSq8TSk9ExkDpjYvS64TSE5ExUHrjovQ6ofREZAyU3rgovU4oPREZA6U3LkqvE0pPRMZA6Y2L0uuE0hORMVB646L0OqH0RGQMlN64KL1OKD0RGQOlNy5KrxNKT0TGQOmNi9LrhNITkTFQeuOi9Dqh9ERkDJTeuCi9Tig9ERkDpTcuSq8TSk9ExkDpjYvS64TSE5ExUHrjckl63NjIz3K9cnBwoPRE5Nogvf39/cE4Y7l6OTo6+kN6Z2dngx2mVg4PD+dlqG1K5fj4eHZ+fn7xbSsisj1v374djDNTKn//+9/nb/aH2qZWTk9PZ3sX93byZNMiIjId8thwV9gZ6YmIiFwXMz0REdkaM71OKD0Rkemh9ERERCaKmZ6IiGyNmV4nlJ6IyPRQeiIiIhPFTE9ERLbGTK8TSk9EZHooPRERkYlipiciIltjptcJpSciMj2UnoiIyEQx0xMRka0x0+uE0hMRmR5KT0REZKKY6YmIyNaY6XVC6YmITA+lJyIiMlHM9EREZGvM9Dqh9EREpofS68TXr18X4qO8fft29vnz50U5PT296CkiIjLMzkjv9evXs8PDw4X0Xr16NXv27Nmi7O/vz/b29hbl4ODgUrvCFBEZHzO9TkRYm0JmWMVWpacwRUTGQendAcYU5ps3bxZjj4+PL8377du3ixXvFlwnWTnXGOp10849Tvn+/ftFLxGRvviLLCPTCpMAn70jwCrEJ0+eXBIm57V9V4XJXo+OjuYlUPfu3bvFPaE8fvx4LkdK7buK9+/fL8YzV/jx48fFkYjcJGZ6nUjwv8sgsio2Anuue5eEyeevHz9+nMssMuKYNwSVobpV0J8fLq4B+VWx0lYL7aGKksK52aXIOCg9uRWmJEykg8yQS34YUlch09sU9jH0g5U56/yIlrkjXMYiYjJNjnNNFfrXbJO+zJn+FOrqGiKye5jpyajC/O233y4JKGK7rvQY34qqwlx1/noeaS0DmSHUup+fP3/Ox1NYG2nWeehLPa8UhMmYTUCYZppyVzDT64TSmyatMPkBqIKLKKhrs6MqmXXQt0qtJe2UZGmBY6TEPpKtVUFlb7wOiZX6du2r7L2F+drxzE99pArskf3mmL3lGkWmgtKTew/BOyATAvyQJK4ijiHxVJgr/5azrg+RSS185gj5bBAQDHO0tPvMNW0D4mI9ShVspFfnpS5745g29p5r5DhwPUN7N6MUuYyZnoxOAnVoHx1CgvymEOBXvZus8zNvpAacLxMme032hwCHZDa0d+pSmL+95mVEdqxZr4c6pEUd+4AIGtr7RebHXFV8tFeRcp65WrJnXodkKbIpZnqdUHq7TSud+uhuUwjSCfQUAjbzks1UMUVKeZyafi0Ign51vjou1LmhFdCmsM+My+PUkD3U+tQB94prr7QZJ9eTsQShVTLLOPZEv3ZukU1ReiJbgASRyZCcKgRpRIAE8uiOsZFDqJIgwCMDCgGevoypWVWgrsq4FQtsKz3mrtJm3lxD6oA+CKzW1eNKuzfWyDqraMfVc/ZU99rC9bdvDER2BTM9mQTJ/BLcKQRdyhgBNkKlILohUQKyqcIYEhz7pI62SGsdkWf2EClHsLlmyB4Q9NCjzkorL/pTt2pf7TVlL4E29gDIL+syJ20puR6o46HdK2PTdxX0aeeSaWOm1wmlJ7dFKxCEwA95gj9Bel2gHpIW8zAemK9KoZXK0HjOa7CJlJDYqiDEnMydNereI3TkST9es0f6RcJAffbHPCH7CByzn9yzwHycpx3yqJV7wxsTmT5KT0T+QivOEOEQNKr0kAv/DjJ1iIG+kV+EUeG89k+21kKfjM1fzgnMnQwzayUbrWIDxuW6aludP/MFjpmvXTcyjfQYt2z/ItfBTE9kJAjcBGwKwRuR5XwbqgSRC7KIiFohsFbkBDWLbGGftY2xeadOW45bWunlvEoO6jmvlNwHjtl/Hvdyn+p15vpkdzDT64TSE7kaSHDoEWHkWUFGybYiKPpUmVKXOem7THpkcfQF6qvUKoiP+SJFYM2sB/yFH/7Sz8uXLxcxgPLp06e52Clfvny56C23gdITkZ0DkdVHsMksW2FFiGRoNfNDXAQ+CvWRarLK/DIS87ZrcZ65mLuu+fvvv8/Fxl6q9J4/f77403dPnz699KfxHj16tGhTmNJipiciW1GzO0BWQ1JDfMgMsUWANcPjNVkm9YiTuZhnG5g/UlOYV4f/GPv8/PzibD1mep3IN52I3D5ICWH1AEmRKW4rvevQU5gnJyeX5p4q7PXhw4fz/W/yv6ooPRGRe8g6YfImoUqxChPJ1DYkUsfepDBfvXp1aW/I/cOHDxetu4+ZnojILXN2dnZJamRPVXo3KUzG1/lTyGT5r8Z4/Fkx0+tEvoAiIvInYwuz/T8zhwqPefl8E5SeiIjsBEPC3N/fHxTdUKFv+89fpo6ZnoiILNgk0+NzPh51RpRmeh1QeiIi/RnK9A4ODua/4MLng+0/Z1B6IiKyszx48GAuPv7JAr+1mT8scFcw0xMRkQXtb2euw0yvE0pPRGR6KD0REZGJYqYnIiJbY6bXCaUnIjI9lJ6IiMhEMdMTEZGtMdPrhNITEZkeSk9ERGSimOmJiMjWmOl1QumJiEwPpSciIjJRzPRERGRrzPQ6ofRERKaH0hMREZkoZnoiIrI1ZnqdUHoiItND6YmIiEwUMz0REdkaM71OKD0Rkemh9ERE5E5wfn4++/z586J8+PBhkYBQXrx4MXv27Nns5OTkYsT0MdMTEbnDbCqulAcPHsz29vbmhePaRt86lrlev349r98VlJ6IyMTpLa46N2tdBR9viojIX5iyuO4TZnoicuv8+PHj4uhPvn//Pjs+Pp4H8amguP6KmV4n8o0hItvx9evXeekNsmp5//797OPHj7Ojo6NLAZK+h4eHi4LkgFf6vXv3bv7K50ZjobjGRemJSHeG5IUYhuoRSBXL48ePB8U0BP0QT513U4FFVGRxrElf5uE145iDucLPnz/nr/SvtOeKS7bFTE/kFkAQraAQQWTEcQRF4bjKIZlQQEQIZIi2rT0nsGetWo+0WCNrsd8ILIKlPmN4Ze5Av1wPx4FxrAfMnfmQK/PTnr2kRKBISnFNCzO9Tig92UUI+gRwgnuFoE8h0AfOI8JWIARu5qnio0/mZezQ52JQJQNVeoyhLRlWHicC66W+Qn0l5209osr+lo0B7hHXR//ss+0v00XpiewAkUsgOFOQAYGXEqkgAs4JxBTqN/0hZyzBvA3iyWyoj1hYI/vKfioIL1IInLOXtm8l0kMs9OM4GRgC5Dzrsa8IMfvOGkNZG+S8zguMz76Wjan9uQ+pZyxrsnf2XdcXuQ5merKzEBApbYZDoG3lVQMm7QTXmklRx1yRQO2PnCKCq5K5CNrtepSaWeU8x5SWVh7MSR17XwZt9GE+1sp6wHiuLfdyaB7ub70HdQ9VVJkfqOfa87Wpx5AxzEtbSl0/ayJAxCfTxEyvE0rv/oEsCHoESArHBNME8QRKjqtQOCcrol8rRGBM5DYE4yvMs6zvOjIXQbvOUaVGfbKw1NXjSru3ddcS6rgqPu4Pbck2IYJh/dxDBIR8gLWSxWX9wLzM19Yzvq5h1nZ3UHoi/08bvBEWgZDCcdqXQeAleNZ3+ARRgmUCdWjPWaNmDBX60g681vlDnQuqZHmlbPJDzn65VsZHnFUouQfU0VYfB2Zspa2jb0TE8ao9tddE3/Rn3lwfJWsk06JwHOjDGIrykl3DTE+uTQJmfSefIEswJuATIOmzaZAk0DJmGTWID0mPgM4rhbmyLnPWYJ8AX6lzQYR1VbIH1oigsh5zZh9AG+tGenVN9k497bmO9poh6wwxdJ2sUWnPl8G+Nu0rdx8zvU4ovelCECRo12/8NiDDUN0y6LsqsNJOhkSQZ/0qyJwzvp2D4M8+GReRIJBKu88hwWzC0Jisx74itUBblWLO6cde6z659vqbnDdJ+6hS7jdKT+4dBOUE8Ty+43idTFZB31ZYFdoRLaWVB6Koj+MqjEMgkWIra8j1VLJ3Mi3aVu0N6NfuCxAV9whp3Ja0RO4zZnpybSKJfDZV6ypXkR7iqtlbS52fflUwyeJa6F/7wVAWx9pDe0/WRdmld7YiPTHT64TSu12+fPkylxrl06dPi6/H27dvLwkojw5rXbiK9MiC6F/nYO38cLXzV/GxPmM555VCO2UoA8znZFeFbI1MERlS6vEqYYvcJZSeTJZl4qK8fPny0p91evTo0eJvFVKePn26aHv+/PmlsQR75gREENG00hsS4SoQB/PUDCuPBPkhax+fIi/Wp7RtIiJwZzM9/tbeVQLsrtBLXGRAmZdyFWm0jxORFeu1WRXyuotfE5H7jJleJxKc1/Ht27d58H/48OE8yE4RAn8VTJXPq1evbkVc14F121/K4BrJuFZBnwiTkseDu/QDJHLfUXq3AFkdNx4hVEEghl5cRVz7+/uX9nVwcHCpvY7lM7I6711+TMc9TBERuQl2OtM7PT2dvXnzZp7VValUuazipsTFPkVE7iJmep2IUID/K4tHe1VCQ+Vvf/vbJTEpLhGRcVF6HUF27Wdcq8ovv/yiuEREZMHOZXp8xoX8+GWVNnMbKiIi0g8zvU5Eei1kb9z0Fy9eDGaB/nstEZF+KL1bhn+ywOdx/Oo7v+DiI00REQk7n+mJiMjtYabXCaUnIjI9lJ6IiMhEMdMTEZGtMdPrhNITEZkeSk9ERGSimOmJiMjWmOl1QumJiEwPpSciIjJRzPRERGRrzPQ6ofRERKaH0hMREZkoZnoiIrI1ZnqdUHoiItND6YmIiEwUMz0REdkaM71OKD0Rkemh9ERERCaKmZ6IiGyNmV4nlJ6IyPRQep34/Pnz7NmzZ4tydHS0ECGFG0+flLOzs4uRIiIif7BTmd6vv/66kNrJyckl6fFOo0rx4cOHs729vUWpbQpzNT9+/Jh9/fp1XjgWEVmGmV4nIqhtqVJTmMt5//797PDwcHZ8fDx7/fr1/Djf0EiQc66z8u7du3kRkfuH0ruDVKmNKcwPHz5cmvv8/Pxixdvj8ePHc7lV2BtQTzvXET5+/Divq2JMH16HJLmKmmWKiIyNv8jSmSq1VpgvXry4JMUHDx4sZMlxbaNvHdtLmMhq2bs21qENkeWxZzJBMkNAVtSFZI6bkMySV+ZEmhwH1qA9e/z+/ftFy2z28+fPiyMRuUnM9DqRYH9fQGJVakiuSq+XMBFJzdIiM+CYgsiQEa+pS79Wcjz2rJnhMpbJMY9NWY95Ilv6s8ecs349B46Zc5NHr3WciGyO0pNbZ1thHhwcXMzwB4xFGvmGrnKLFAEhpT7yqWWTR5XMhciGQEjM02ZzrJtMkHXrXiHy5jpWkflDZFlFiDhZn8J8PoIV2U3M9GRBgnqlPq6s0kMukRTtEUvtA9QjlHWZ1Co5Rr4tdW/sh88Xc84Y6jjfRE5VegiuHZd26jiOZDnOY1ZeqaOwdubJPaFQl3m5fxyniOwiZnqdUHr9SUAne+I4ssnjQb6xh4IzfVKf4F6p7ctg3WV9mI85Wuif+qzB3pFJHoVusjbUfoyt18F8+aGua0LWClW8vIGgf8YkI868ude0Ueo86+Da6meaIreF0pOdhmBKUOabmJIMDjaRHoE70gSkQHBfB3NHBi2RRgvr5Icte0AErFfFMrTnliqjzBUJ8Zr7wBuAKqf2nDnYV0u9R2GT+7IM5mvH5z7V+4h486bFR7MiZnoyAjXjiAQIvrwipU0yEmQbWSUoV3kyHwIN1NM/MqI960SEsKlYmIdxrBFJMCfUOdgf18Urpa4LqW9p9xE551qvAntlD5RcPzAP+6lrUUe/HNOWfVPqXmuWGthnve+VdY+s5X5gptcJpXc/QDgEXgJ15AMJ6CkE7xqMW6mEZfUtBPesm2DOOWvUH2gkgYypZ25EUdlUehFQricS2wT6ITvuT90bdcl+c2/qfnIPA5kfc9X9cp6xtNOfcUOwf/rnOpb1k7uN0hMZoGYn/IAQnHN+lYyBvkPBdeiRIrSyWUWCd0hAr9lUlQj1tCOHwLVViYR2H62ANiVyBu5FnSN7q/V1v8nCK/Ste8t5hFffWLTUcUNZosgUMdMTuQBh1ewSWbSyakVAnyoS2lspt2KBbaXHHlkvMmPePF5NHdAHKde6elxp90Yf9rbsjQQsk2VgT3WvLVw/Y2T3MdPrhNKT61Azy3o8FJBXQTCvmR3wA0+mwyuBH2FERDAkONalL6+Ilj51zBARC30pydwiaubK9SSz5lqHHnVWqqyAPtTVDLcl15S9cO1VkrRlPG1Zl2ukLSXXA+y3UvfKtXOd1K2TJfO1c0k/lJ6I/IVWaIgTGRHEU9YFjvSrIBbkAYyPQKCVytD4dt08pqzzDsGczI3oeK1zICfGcn304zVzIaOaKVOf/TFPiBwByWW+zBXxMR/nlOwhb2q4hvYNioiZnsiEINgjgYigsiwTTFbTSg9B8Jd2UocY6Bv5RRghookoaE8W2cKcGcs4hBURMXcyzKyVearYgDlyXbWtzs911SySY+ZsP0eMTCM9+qzKVmUczPQ6ofTkPoAAIgpKAjhlG6oEmTuPCIeEQOCqdUislVRgnrqnZHeAfJYFwXa+nFfJQT3ntd4Hzlkv+6OtXmeuT24GpScid4ZkYS3Isf3nGjUji6CQD/XJ9KjjOI93l0mPuekLvLZrBcQX4aY/a2Y9+O233xZ/Z/bly5eLN9CUT58+za+F8uXLl4sRcpcx0xORLiSzrFkYICcKgqyZH+JCkBTqk60hJM6RJHMxJ3NXISPRzMXcdc3ff/99ITbGVuk9f/58IcSnT58u/qcSyqNHjxZt90mYXM+3b98uztZjpteJfKOJyN2hZneArJAZEqtSI6NDgggtMqwZHq/5TI96gjBzXecXWVgzUqPcF2GyP66Ba0Jo61B6IiIbgJQQVg8QFHK8jvSuwy4Lk/Xa/bx582Z2enp60WO3MdMTEZkQty1MsrY6Zy2sfXJyctHzD8z0OpEvmoiIDDOGMB8+fHipfqjs7+/PM2nWU3oiIrJzRJgHBweDoltWkN/5+fnFLNPHTE9ERBY8efJkUG4ptPPYlMecZ2dnZnq9UHoiIv3h0WWVHOdIDbmRDbYoPRER2Vn4TO/FixfzzwOv8u/1dgUzPRER2RozvU4oPRGR6aH0REREJoqZnoiIbI2ZXieUnojI9FB6IiIiE8VMT0REtsZMrxNKT0Rkeig9ERGRiWKmJyIiW2Om1wmlJyIyPZSeiIjIRDHTExGRrTHT64TSExGZHkpPRERkopjpiYjI1pjpdULpiYhMD6UnIiIyUcz0RERka8z0OqH0RESmh9ITEZGd5vPnz4tycnKySDooCO7Zs2eL8vDhw9nbt28vRk4fMz0RkTvIVcW1t7e3KLXt6Ojo0lgyuzr3u3fvzPR6kBsuInJfqHLpKa6zs7OLFa+OjzdFRG6YHz9+zN6/fz8P4D9//ryonc2zEAL+69evL2pulvPz80ty+fDhwyX5vHjx4pKcHjx4cCviuk+Y6YnIAuRRpREIqojj8PBwXo6Pjy9arsf379/na1Y4R1asGagjm2APjx8/nu+BsfDx48fFnmjP3ujPPPSL/LbhOuLiuLbRt45lrjo3a+0aZnqdyDeJiGwOsvj69eu8tBCoEEagL/IYkh4ioX/mQiCt+Aja1EVGIcKhjblpZx3qeEViZGlQBcZ6HIfar0qM13odgf7ZL4Vz4FhxjYfSE5HRIVC3EPhb8SCA9I1UUgj69TEf8qEukiJwMecQzFkF1J6zFnMznvpIiHrq6I+wshbrJsOjLUJiLOII9Tx9IOIE1qKN8wgw18b9Sck87FNx3V/M9EQ6Q3ZDIZjWILxMMC2RQvtuOkG+iow6+kMrMfZAO2sHREQdomCuZbSSY+7MQ1sdy3WmL/teloVVct7W517BsjGVCBB4jVilH2Z6nVB6clsse0SIUBAOAZ/MglcKwTaFc/oRuCMcCnMl61lHMqg2iDMP9cybuVgv+8xalWRAFeanbtV+mJM+rMVrDXKsQV2uP9cMSIgxKRFgu4ec06fe5yruZWOYE9Ei9awHETr74xo53vSey+YoPZGJ0wY+AmaCMdCeH2ICOMGylkA/zhOk62vtBzX7uSqMQ3YE7iqxSK3Onbr2uNLKI0LgdRlca8bl+lZd7xBVSMyFpAA5cZ9znLlrf+C4fu2yH66frxdztNdAf6RJHxEw05NJkyyrhbq2noCX4LkK+tXgSDCtIqiyqJkGtOeMJVC3tGKJGOib+es8y+D6E/g5XrfPWlePK3WOiBt4rfJvqeNyPfkacMxanFeJsS/qqeM494r+Ke3XLOPpXyVXj2U6mOl1QundTQjUBDhKlUCCagoBNwETOKe+kn7rIHjWfoxj/QRVjpMZtOKIXELbHtp9cD3UpT9zbBLE6cf+Mo45ch9oy9oRYiQDQ9klbTVA1WvNfVm2r6FrYnwyNubmnNfMwWv2WedlXxGm7DZKT+48NVgRyAiaBDXKpo+RksEwhvko/OBkbtqq5Kgn6DIOaCdbYzzQl/M2MC+jBncCdcYDc2cd5qc+7bRVct0tQ4LYdG8V9sa6iIM52Ef2wDntgT6skeugnb680kbfun/mquOBTG9ZtleldV1Yl32J3DRmejIPfgneNYAjoTYoElD5t0sJWATRZBcUzplvHRkzxFCGAqydgE57+pFpsM9kOxHWKjIX11HnhCqnXFNekUcl190ytI9tpDc0JtceqVW4D3nXjaQ4zx6VjPTATK8TSq8PCZIExAgg0qItQTMQZCkJoPSv7Qmw6yCYLwvCjG+DOdA/9dlDshVKrV8HQqAvEqvXy3FduxUwx1XqyaBahvbBNXNt9Kd9aFyFPQ4FE2SaR4qbCF6kJ0pPdgaC6qrsIwJL0EcASK4GdNqTMaXvJoF4lfRa8QT6p77ugWP2luNl87bQN8KHrMt1hLpm4Jy+iIfxXEv7Q895+ziQeZkvpd6nWs/c9M01ich4mOndc5J9EGxbqKeQzSA2MhMCNa/Up0/OKfRjznXiQxzLgjpjh2TM3BRgrZpxBeYdupYhstcK53VexJU1K1lj07WWgThz77jPzJeSbE5kypjpdULp9YGgnsdtBPwa9BOMgfY8jqv1vA5lOe1nXy20M2dLxjFHnZc9sbfItO6hwh7XrV25rrRE7jtKTyYDgiCbSkEGefNA+fXXXy96/gm/uZfsJ4/ZIHNAFQ7SaOWFeIaysBZ+UBjLXByzbs2qqI+Qea2PC9lL+/iwheuvmRPXz5xmUCL3lzub6X358mX+R2R3nXXiev78+eKvvz99+nTxl+Epjx49uvTX4V++fHlp7H/+859F5hQ4j/SGhAaII3KiD/2roK7yrg9xRUpjwr1iTxQEnDXGXkfkvmOm14kE6lXwl9GRwpMnT+ZBHyFMgdPT00vievv27SX5VDFdVVyfPn1azIvor0qyqIiM82RpCIK2FkSVR4hkTRxHKK1EU09hj6yTsSKy+yi9W+Dbt29zGQz9d/ljcRVxHRwcXNrH/v7+pfZXr15dGlvn3UZc1yGZJDLidUySaVGq/NY9lhQR6cVOZ3o8vmwzo1rI+Co3JS4Cu4jIfcBMrxORC+J68+bN/LFfldJQ+eWXXy6dKy4RkXFReh0hs0NcVWTrioiISNjJx5t8hscvQ/Bf/ref47VFRET6YabXiSq9Fh5F8vkcv6354MGDS9LjcaiIiPRB6U0AfgMSQfK5ndITEZFwJzI9ERG5Hcz0OqH0RESmh9ITERGZKGZ6IiKyNWZ6nVB6IiLTQ+mJiIhMFDM9ERHZGjO9Tig9EZHpofREREQmipmeiIhsjZleJ5SeiMj0UHoiIiITxUxPRES2xkyvE0pPRGR6KD0REZGJYqYnIiJbY6bXCaUnIjI9lJ6IiMhEMdMTEZGtMdPrhNITEZkeSq8TJycns729vXl5+PDh7NmzZ4vCDY8UKfT9/PnzooiIiMBOZnpnZ2eXpMY7jSq9o6OjS1KMLBWmiMi4mOl1IlK6LgrzZvn+/fvs69ev8yIidw+ld4dRmFeDa+SeHB8fz48fP348e/fu3byNOtpaqM/1//jxY/4qIjIW/iLLDXHfhImwkFwLmR8gN9p5Da9fv55f7/v37+fnh4eHi0LfXXo3KXJfMNPrRAL8fWRXhVkzuxYERxtCAyQZweVRaD2G9nwVkSr3Jq8RLrA2fSi1HswwRTZH6cmk6CnMT58+Leb98uXLxYp/8vHjx7mokA6vyeAgAmNOxue1io1xOf758+f8vBXUEIiMeRgTWDsCpo31mJt65m33RqnkWjZZX0Smi5meLGWdMJ8/f74Q4tOnTy8J8+3btxez/EErFwSCdJiX+rxTTD1QX0v6rIO+y+SEENt56MuYEOlVEWYPmxBB1kIdkOFWGbdw7RSzTdkVzPQ6ofR2i99///3i6E8I+EgHEEHkxnGCfBVLKxl+sJhjFcs+SwyshWhb2v3QhywY2DNZ4ibSi8QzF1A3JPUKdYyjXzLVNpAgYdooVcgit4nSE/l/EriRBQE9mV4yMNqGqGJpJcM8y8aFNmtroW1IOlVGGZ+6rLlq3oAo8xh1iLpOhfpkg4G6yA3ZE1gYG4lGytSxN/rzSlkWhPLGQ3HKfcVMT7pBcCa4EpwJtvWRXQJ2S8QSeRHQGcc5Y5hvHYxb9nhwmXQYEyFnD8nuuI51GWSgz9D8gfZ2b5FWC1LKfRqaN49Jqee6NiEipSDGTceJLMNMrxNK736QwE5AR5QE5ZRVGVQlWWaVS7Ia2tofUOatwb8KKH03Fcsm0mtp1w91zQgKAbdwbZvsDehX99eeA+d5AyCyDqUnMgEiAiTDa/2h5DxZI2KtoiLY095SBbQK5o1ghxiS3jJptfWRI3OwTsTEdVCXQtuyIER73gzkN2JznmvnnrRz5JdzqGcfke+YcuQ6Vv2Sj8gYmOnJzoEMCL4UAjTBOeebgsQIsq2gCLrUt9B/k/mRASKpGRnCiBxoa8mj0zbgs96y7DbXDOy33fMyebAO4xAYpV4/9XXftLNv6jjmHuQ+ZL3UB86rRLPPdn+sS127Xs3OZTcw0+uE0pNdgUBOoEcwkQwSQg6cE9wpBP1ILY9yGUs/ggjnEPFUknkB87RSWQbrA9LJccheUzjPXqoc63rsoUqvzpnrAebIGObmemlLPfPzT12yB9kdlJ7IHSZCo/CDnkeBlE2zFCSRUn9jk8DPnMiiZnhIDxlQjyBYk/MIhbWpY751e6hSYkwNVrWt0oqNMVmbtkrmyKNYromxvKZv6kOy0mXri4yJmZ7IjhB5VGEAgkF8yCQlWVWF8RFP4DyCRWZVgjVLq2syJhJsRZXzyJlxHFOyTvZBWSbd/GGErCPTxUyvE0pP5Pq0n/WRGVZBchwhIVJIpklgo57jyIjzZKvJ7gDZrQuErE2fyDBjAeHxl34ODg4u/aWf/f39xV8Borx69WoRGyiMS1GYN4PSE5E7B4JCIrwipwgFuXCOIBFmRAkcp77KjbpIibEcZ951nJ6eLqRG4c/dVelVISpMGcJMT0SuRPtIcxVkiciuSgSpIEIKxwEZIr5eWcNNCfPbt28XK+4mHz58mGdv5+fnFzWrMdPrRL65ROR2QVb3jasI88mTJ5eEyXltf/PmzWJcxJ8yBWGyL/bN/7Ly8uXLtXtSeiIisgBpVLEhukgPAVYhTkGYiK7ugcL/qEIGeBcw0xMRmSi3IUyytjpPLY8ePZrPQ+YbzPQ6kS+WiIis5zrC3KQk+1N6IiKysyDBIckNFX7hB5lu+ksvU8BMT0REFvCbqkOCo5AR8pkfGR5/QADM9Dqh9ERE+sPndpEcxwgNsdXP8SpKT0REdhb+vST/tnKs3wadGmZ6IiKyNWZ6nVB6IiLTQ+mJiIhMFDM9ERHZGjO9Tig9EZHpofREREQmipmeiIhsjZleJ5SeiMj0UHoiIiITxUxPRES2xkyvE0pPRGR6KD0REZGJYqYnIiJbY6bXCaUnIjI9lJ6IiMhEMdMTEZGtMdPrhNITEZkeSk9ERGSimOmJiMjWmOl1QumJiEwPpSciIjvHly9fZp8/f56XT58+LRINysuXL2fPnj1blEePHs329vYWBfHtCmZ6IiJ3hOuI6+nTp4u258+fXxr77t27xbyUHz9+XKxopteN3HwRkbvMbYjrOig9EZF7zq6J6z5hpicitwaB+/3797Pv379f1MxmHz9+nB0dHc2zh1p/0yiuzTDT60S+YURkc75+/To7Pj6eF+QyBgTpIRkRwAnoNYgjL+oODw9njx8/XuyBPtRlbwma9I3skB99rkOVi+Lqg9ITkVFAEJECJRBgIhEKYhkSGsKjPePp9/r164vWP0EwbdBmPtahP0EdmKuuXQMddZyzDscIK/VZM/sB+g3tJWPpS+Gc1//+97/zfZycnFySD2tWcT18+PCSuGqb4hIw0xMZCYImAbqy6vFcFQhCqn3zeC/BH0FEbLRVCa4ikgn1nP0yF4V9RGLMnbWRQRVYlStzsWf60RYYk3Ne6z3J+qxNWwoCAtq5VvZAyXpkaYiLvVZxkWVUcZ2dnc37y81hptcJpSc3CcF8lbAqCfIpBG6CeCuDSqQW6B8hsG6Oh4gQNqHOU2UE7CFCA9pYm/pIqEJ7FVjO2UubtWXdZWMqnNM/aw9lrTJdlJ7ILUIAHXpMleDcBlQCLYG4rScIU78O1qJvG8gjzCqdsExqBI6a8bDfdl6gnvaUof0H2hESfVrh0IZkUmhnb5EP5xkP7ficUzgOuSewbAxrsGf6ZjzHuTfJ9qgfErDItpjpyZ2gBvaUwDFBnCCKWAiqBFfgte1PMKZfrVsGc7ZZTiXBv5Jg3lIzsVY8lIht3ZoVxpHNUZhrSECr+Pnz5+Ia2E8yw4gqcJzslnvHHoHXKi3m4NqYN18z+ufrAciPa40UZdqY6XVC6e0+BLoKATTBEdogz/km8APX/tAlmDN/20YQrnOzVjILYA/to8Bl0KdeQ0uEUWH9oblbkVS4V5kLGWyyN6Bf7kXmX3ZvIkfquR+sw2v6UM8emIP7l3kAOUViVXJ8zduvu9wtlJ7cWxAFwbCWMCQxAmSVAsf1nX0N0Kuoc7TQNjRHXYtjAjPrEfT5ASbzWDVvoO866bXZyrK5kUWCRzuG84yJvHJMqZlSpb2HGZv+ERUl1829QHhcF1/TwPmqaxXZBcz0ZBQSlGuQJHAmuCaoUgf047wG/xqgI6E2+LfQf0gggbYa9ENdK+PZW72GVfMGxrQyr9R1KtTXjKi9f7QzL5KJmHLvcs20U09ZJqOMGQOlJ0OY6XVC6U2bZY/sAm1kUenDD0krrARwCvUUxqyiZkBDMNc66XEcalBfNW+F8fWHHtFkTl7ziJBjSkRepcVaVVD0QYDsp76REJkaSk/uJZEP3/wEaoSS4A4EdupoS+YCjKnyqQGe+k3EQ58hMZAtRjiV9vFi9tayydqBNZAYhePshzcDHDM/ZegxZL1PItIXMz0Zhf/973/z4F2lhjSSqVWxUJ+sptZHmJVlQqowVxUfYqlzMUdEnL41q0JUQzJCWP4ShshqzPQ6ofT6w1+zQBwpfDPnvlOQQ/2zTg8ePFj8uad//vOfF7P8CWJBOIAIq5RClRpiiqio4zjj15FHp3lkWD8vA84jwuxjU7iOmsVxLTkXue8oPblVriquSIvC3y2sbXwj17H83cM69/n5+cWqfzxKpK5SHy1WoVUQVLKuSI498sp4EZExuZOZHoGfd/ZDQXYXuElxjQmPDZFVSn33x9dj6JdSeCS67hFiPptLdlWP62NKEbl5zPQ6kaC9im/fvs3/u5D8pXWC/m2xq+K6Lkhs3WdwInJ3UHo3DI/YuOn831dVHBSEcR3uq7hERO4qO5vpnZ6ezt68efOX//ixlv39fcUlItIRM71OVNHwn0FWOa0qiktEpB9KryMIisys/qr8uiIiIhJ29vHmly9f5r+dSfY2JLuU+mv1IiIyLmZ6nWil1/Lp06f5Z3ztL7Tw2Z+IiPRB6U0AfnmFR6GvXr3y7xqKiMiCO5PpiYjIzWOm1wmlJyIyPZSeiIjIRDHTExGRrTHT64TSExGZHkpPRERkopjpiYjI1pjpdULpiYhMD6UnIiIyUcz0RERka8z0OqH0RESmh9ITERGZKGZ6IiKyNWZ6nVB6IiLTQ+mJiIhMFDM9ERHZGjO9Tig9EZHpofREREQmipmeiIhsjZleJ5SeiMj0UHoiIiKTZDb7P2hkd+0yzNRtAAAAAElFTkSuQmCC)

Figure 7: Copy file (local to remote) sequence

In the frames in the preceding figure, the remote file is first created with the [SMB_COM_NT_CREATE_ANDX request](#Section_8e14ed93f27544d1bc46dfaf296c91b1). The data from the local file is then written to the remote file and, subsequently, the file is closed.

NT_CREATE_ANDX

- Client -> Server: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2288 (0x8F0)
- SMB: Command = C NT create & X
- SMB: Desired Access = 0x00030197
- SMB: ...............................1 = Read Data Allowed
- SMB: ..............................1. = Write Data Allowed
- SMB: .............................1.. = Append Data Allowed
- SMB: ............................0... = Read EA Denied
- SMB: ...........................1.... = Write EA Allowed
- SMB: ..........................0..... = File Execute Denied
- SMB: .........................0...... = File Delete Denied
- SMB: ........................1....... = File Read Attributes Allowed
- SMB: .......................1........ = File Write Attributes Allowed
- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. =
- CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File Share Access = 0x00000000
- SMB: ...............................0 = Read not allowed
- SMB: ..............................0. = Write not allowed
- SMB: .............................0.. = Delete not allowed
- SMB: Create Disposition = Overwrite_If: If exist, open
- and overwrite, else create it
- SMB: Create Options = 68 (0x44)
- SMB: ...............................0 = non-directory
- SMB: ..............................0. = non-write through
- SMB: .............................1.. = Data is written to the file sequentially
- SMB: ............................0... = intermediate buffering allowed
- SMB: ...........................0.... = IO alerts bits not set
- SMB: ..........................0..... = IO non-alerts bit not set
- SMB: .........................1...... = Operation is on a non-directory file
- SMB: ........................0....... = tree connect bit not set
- SMB: .......................0........ = complete if oplocked bit is not set
- SMB: ......................0......... = no EA knowledge bit is not set
- SMB: .....................0.......... = 8.3 filenames bit is not set
- SMB: ....................0........... = random access bit is not set
- SMB: ...................0............ = delete on close bit is not set
- SMB: ..................0............. = open by filename
- SMB: .................0.............. = open for backup bit not set
- SMB: File name =\\filename.txt

NT_CREATE_ANDX Response

- Server -> Client: SMB: R NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2288 (0x8F0)
- SMB: Command = C NT create & X
- SMB: Oplock Level = Batch
- SMB: File ID (Fid) = 16392 (0x4008)
- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. = CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted

SMB_COM_WRITE_ANDX Request

- Client -> Server: SMB: C Write Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2384 (0x950)
- SMB: Command = C read & X
- SMB: File ID (Fid) = 16392 (0x4008)
- SMB: File offset = 0 (0x0)
- SMB: Data length = 1596 (0x63C)
- Data = 00 90 27 66 6D BE 00 90 27 D0 C4 6F 08 00 45 00 …

SMB_COM_WRITE_ANDX Response

- Server -> Client: SMB: R Write Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2384 (0x950)
- SMB: Command = C read & X

SMB_COM_CLOSE Request

- Client -> Server: SMB: C Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2400 (0x960)
- SMB: Command = C Close
- SMB: File ID (Fid) = 16392 (0x4008)

SMB_COM_CLOSE Response

- Server -> Client: SMB: R Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 2400 (0x960)

## FSCTL SRV COPYCHUNK

The following example refers to the sequence of operations for a file copy in which the source and the destination are on the same server. The [FSCTL_SRV_COPYCHUNK (section 2.2.7.2)](#Section_bdcc7363d0c74417b45ae46934b11419) is used. The following sequence assumes that the SMB connection to the server, SMB session establishment, and other operations have been completed.

![Copy file (from/to same remote server) sequence](data:image/png;base64,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)

Figure 8: Copy file (from/to same remote server) sequence

The initial step in the preceding sequence is to open the source and the destination file using NT_CREATE_ANDX command. This step is followed by the FSCTL_SRV_REQUEST_RESUME_KEY request. This is sent as an NT_TRANSACT_IOCTL with the file ID of the source file. The server responds with the [FSCTL_SRV_REQUEST_RESUME_KEY response (section 2.2.7.2.2.2)](#Section_c2571af45f264bfcba6738d26f16effc). A 24-byte server copychunk resume key is returned.

NT_CREATE_ANDX Request (Source)

- Client -> Server: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 384 (0x180)
- SMB: Command = C NT create & X
- SMB: Desired Access = 0x00020089
- SMB: ...............................1 = Read Data Allowed
- SMB: ..............................0. = Write Data Denied
- SMB: .............................0.. = Append Data Denied
- SMB: ............................1... = Read EA Allowed
- SMB: ...........................0.... = Write EA Denied
- SMB: ..........................0..... = File Execute Denied
- SMB: .........................0...... = File Delete Denied
- SMB: ........................1....... = File Read Attributes Allowed
- SMB: .......................0........ = File Write Attributes Denied
- SMB: NT File Attributes = 0x00000000
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................0..... = Not Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. =
- CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File Share Access = 0x00000005
- SMB: ...............................1 = Read allowed
- SMB: ..............................0. = Write not allowed
- SMB: .............................1.. = Delete allowed
- SMB: Create Disposition = Open: If exist, Open, else fail
- SMB: Create Options = 2097220 (0x200044)
- SMB: ...............................0 = non-directory
- SMB: ..............................0. = non-write through
- SMB: .............................1.. = Data is written to the
- file sequentially
- SMB: ............................0... = intermediate buffering allowed
- SMB: ...........................0.... = IO alerts bits not set
- SMB: ..........................0..... = IO non-alerts bit not set
- SMB: .........................1...... = Operation is on a non-directory file
- SMB: ........................0....... = tree connect bit not set
- SMB: .......................0........ = complete if oplocked bit is not set
- SMB: ......................0......... = no EA knowledge bit is not set
- SMB: .....................0.......... = 8.3 filenames bit is not set
- SMB: ....................0........... = random access bit is not set
- SMB: ...................0............ = delete on close bit is not set
- SMB: ..................0............. = open by filename
- SMB: .................0.............. = open for backup bit not set
- SMB: File name = sourcefile.txt

NT_CREATE_ANDX Response

- Server -> Client: SMB: R NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 384 (0x180)
- SMB: Command = R NT create & X
- SMB: Oplock Level = II
- SMB: File ID (Fid) = 16386 (0x4002)

- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. =
- CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File type = Disk file or directory

NT_CREATE_ANDX Request (Destination)

- Client -> Server: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 480 (0x1E0)
- SMB: Command = C NT create & X
- SMB: Desired Access = 0x00030197
- SMB: ...............................1 = Read Data Allowed
- SMB: ..............................1. = Write Data Allowed
- SMB: .............................1.. = Append Data Allowed
- SMB: ............................0... = Read EA Denied
- SMB: ...........................1.... = Write EA Allowed
- SMB: ..........................0..... = File Execute Denied
- SMB: .........................0...... = File Delete Denied
- SMB: ........................1....... = File Read Attributes Allowed
- SMB: .......................1........ = File Write Attributes Allowed
- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. = CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File Share Access = 0x00000000
- SMB: ...............................0 = Read not allowed
- SMB: ..............................0. = Write not allowed
- SMB: .............................0.. = Delete not allowed
- SMB: Create Disposition = Overwrite_If: If exist, open and overwrite,
- else create it
- SMB: Create Options = 68 (0x44)
- SMB: ...............................0 = non-directory
- SMB: ..............................0. = non-write through
- SMB: .............................1.. = Data is written to the file sequentially
- SMB: ............................0... = intermediate buffering allowed
- SMB: ...........................0.... = IO alerts bits not set
- SMB: ..........................0..... = IO non-alerts bit not set
- SMB: .........................1...... = Operation is on a non-directory file
- SMB: ........................0....... = tree connect bit not set
- SMB: .......................0........ = complete if oplocked bit is not set
- SMB: ......................0......... = no EA knowledge bit is not set
- SMB: .....................0.......... = 8.3 filenames bit is not set
- SMB: ....................0........... = random access bit is not set
- SMB: ...................0............ = delete on close bit is not set
- SMB: ..................0............. = open by filename
- SMB: .................0.............. = open for backup bit not set
- SMB: File name = destinationfile.txt

NT_CREATE_ANDX Response

- Server -> Client: SMB: R NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 3592 (0xE08)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 480 (0x1E0)
- SMB: Command = R NT create & X
- SMB: Oplock Level = Batch
- SMB: File ID (Fid) = 16387 (0x4003)

- SMB: NT File Attributes = 0x00000020
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................1..... = Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. = CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File type = Disk file or directory

FSCTL_SRV_REQUEST_RESUME_KEY Request

- Client -> Server: SMB: C NT Transact, Dialect = NTLM 0.12
- NT IOCTL Function Code 0x00140078 FSCTL_SRV_REQUEST_RESUME_KEY
- File ID (Fid) = 16386 (0x4002)

FSCTL_SRV_REQUEST_RESUME_KEY Response

- Server -> Client: SMB: R NT Transact, Dialect = NTLM 0.12
- NT IOCTL Function Code 0x00140078 FSCTL_SRV_REQUEST_RESUME_KEY
- File ID (Fid) = 16386 (0x4002)
- Key = 2D 0B 00 00 01 00 00 00 59 84 0C 62 1B 84 C6 01 08 0E 00 00 00 00 00 00
- ContextLength = 0

This is followed by an FSCTL_SRV_COPYCHUNK request. The request uses the resume key generated earlier.

FSCTL_SRV_COPYCHUNK Request

- Client -> Server: SMB: C NT Transact, Dialect = NTLM 0.12
- NT IOCTL Function Code 0x001440F2 FSCTL_SRV_COPYCHUNK
- File ID (Fid) = 16387 (0x4003)
- Key = 2D 0B 00 00 01 00 00 00 59 84 0C 62 1B 84 C6 01 08 0E 00 00 00 00 00 00
- ChunkCount = 1 (01 00 00 00)
- Reserved = 0 (00 00 00 00)
- List:
- SourceOffset = 0 \_(00 00 00 00 00 00 00 00)
- DestinationOffset = 0 (00 00 00 00 00 00 00 00)
- Length = 1731 (3C 06 00 00)

FSCTL_SRV_COPYCHUNK Response

- Server -> Client: SMB: R NT Transact, Dialect = NTLM 0.12
- NT IOCTL Function Code 0x001440F2 FSCTL_SRV_COPYCHUNK
- File ID (Fid) = 16387 (0x4003)
- ChunksWritten = 1 (01 00 00 00)
- ChunkBytesWritten = 0 (00 00 00 00)
- TotalBytesWritten = 1731 (3C 06 00 00)

The final step is to close the source and the destination file with SMB_COM_CLOSE commands.

SMB_COM_CLOSE Request (Source)

- Client -> Server: SMB: C Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 640 (0x280)
- SMB: Command = C Close
- SMB: File ID (Fid) = 16386 (0x4002)

SMB_COM_CLOSE Response

- Server -> Client: SMB: R Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 640 (0x280)

SMB_COM_CLOSE Request (Destination)

- Client -> Server: SMB: C Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 656 (0x290)
- SMB: Command = C Close
- SMB: File ID (Fid) = 16387 (0x4003)

SMB_COM_CLOSE Response

- Server -> Client: SMB: R Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2049 (0x801)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 656 (0x290)

## TRANS TRANSACT NMPIPE

The following example illustrates how the TRANS_TRANSACT_NMPIPE is used.

![Named pipe request sequence](data:image/png;base64,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)

Figure 9: Named pipe request sequence

The first frame contains the NT_CREATE_ANDX request to the named pipe. The TRANS_TRANSACT_NMPIPE is then issued against the file ID assigned in the NT_CREATE_ANDX response.

NT_CREATE_ANDX

- Client -> Server: SMB: C NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 2292 (0x8F4)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4048 (0xFD0)
- SMB: Command = C NT create & X
- SMB: Desired Access = 0x0002019F
- SMB: ...............................1 = Read Data Allowed
- SMB: ..............................1. = Write Data Allowed
- SMB: .............................1.. = Append Data Allowed
- SMB: ............................1... = Read EA Allowed
- SMB: ...........................1.... = Write EA Allowed
- SMB: ..........................0..... = File Execute Denied
- SMB: .........................0...... = File Delete Denied
- SMB: ........................1....... = File Read Attributes Allowed
- SMB: .......................1........ = File Write Attributes Allowed
- SMB: NT File Attributes = 0x00000000
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................0..... = Not Archive
- SMB: .........................0...... = Not Device
- SMB: ........................0....... = Not Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. =
- CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File Share Access = 0x00000003
- SMB: ...............................1 = Read allowed
- SMB: ..............................1. = Write allowed
- SMB: .............................0.. = Delete not
- allowed
- SMB: Create Disposition = Open: If exist, Open, else fail
- SMB: Create Options = 4194368 (0x400040)
- SMB: ...............................0 = non-directory
- SMB: ..............................0. = non-write through
- SMB: .............................0.. = non-sequential writing allowed
- SMB: ............................0... = intermediate buffering allowed
- SMB: ...........................0.... = IO alerts bits not set
- SMB: ..........................0..... = IO non-alerts bit not set
- SMB: .........................1...... = Operation is on a non-directory file
- SMB: ........................0....... = tree connect bit not set
- SMB: .......................0........ = complete if oplocked bit is not set
- SMB: ......................0......... = no EA knowledge bit is not set
- SMB: .....................0.......... = 8.3 filenames bit is not set
- SMB: ....................0........... = random access bit is not set
- SMB: ...................0............ = delete on close bit is not set
- SMB: ..................0............. = open by filename
- SMB: .................0.............. = open for backup bit not set
- SMB: File name =\\srvsvc

NT_CREATE_ANDX Response

- Server -> Client: SMB: R NT Create Andx, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 2292 (0x8F4)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4048 (0xFD0)
- SMB: Command = R NT create & X
- SMB: Oplock Level = NONE
- SMB: File ID (Fid) = 16385 (0x4001)
- SMB: NT File Attributes = 0x00000080
- SMB: ...............................0 = Not Read Only
- SMB: ..............................0. = Not Hidden
- SMB: .............................0.. = Not System
- SMB: ...........................0.... = Not Directory
- SMB: ..........................0..... = Not Archive
- SMB: .........................0...... = Not Device
- SMB: ........................1....... = Normal
- SMB: .......................0........ = Not Temporary
- SMB: ......................0......... = Not Sparse File
- SMB: .....................0.......... = Not Reparse Point
- SMB: ....................0........... = Not Compressed
- SMB: ...................0............ = Not Offline
- SMB: ..................0............. = CONTENT_INDEXED
- SMB: .................0.............. = Not Encrypted
- SMB: File type = Message mode named pipe

SMB_COM_TRANSACTION Request

- Client -> Server: SMB: C transact TransactNmPipe, Dialect = NTLM
- 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 2292 (0x8F4)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4096 (0x1000)
- SMB: Command = C transact
- SMB: Data bytes = 76 (0x4C)
- SMB: Data offset = 84 (0x54)
- SMB: Setup words
- SMB: Pipe function = Transact named pipe (TransactNmPipe)
- SMB: File ID (Fid) = 16385 (0x4001)
- Data = 00 90 27 66 6D BE 00 90 27 D0 C4 6F 08 00 45 00 ……

SMB_COM_TRANSACTION Response

- Server -> Client: SMB: R transact TransactNmPipe, Dialect = NTLM
- 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 2292 (0x8F4)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4096 (0x1000)
- SMB: Command = R transact
- SMB: Data bytes = 120 (0x78)
- SMB: Data offset = 56 (0x38)
- DATA = 00 90 27 D0 C4 6F 00 90 27 66 6D BE 08 00 45 00 ….

SMB_COM_CLOSE Request

- Client -> Server: SMB: C Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4112 (0x1010)
- SMB: Command = C Close
- SMB: File ID (Fid) = 16385 (0x4001)

SMB_COM_CLOSE Response

- Server -> Client: SMB: R Close, Dialect = NTLM 0.12
- SMB: Tree ID (Tid) = 2048 (0x800)
- SMB: Process ID (Pid) = 65279 (0xFEFF)
- SMB: User ID (Uid) = 2048 (0x800)
- SMB: Multiplex ID (Mid) = 4112 (0x1010)

# Security

The following section specifies security considerations for implementers of the Server Message Block (SMB) Protocol.

## Security Considerations for Implementers

The CIFS Protocol contains support for NTLM but lacks support for new authentication protocols. The extensions defined in this document offer support for increased security in remote file and printer access via SMB.

In addition to the NTLM challenge/response authentication support, as specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b) section 3.1.5.2, these extensions enable support for Kerberos or any other protocol that can be encapsulated inside the extensible authentication package, as specified in [\[RFC2743\]](http://go.microsoft.com/fwlink/?LinkId=90378) and [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461).

Extended message signing uses the HMAC_MD5 algorithm, as specified in [\[RFC2104\]](http://go.microsoft.com/fwlink/?LinkId=90314), to alter the user's session key.

The protocol does not sign oplock break requests from the server to the client if message signing is enabled. This can allow an attacker to affect performance but does not allow an attacker to deny access or alter data.

The algorithm used for message signing has been shown to be subject to collision attacks. See [\[MD5Collision\]](http://go.microsoft.com/fwlink/?LinkId=89937) for more information.

The new "previous versions" feature potentially allows access to versions of a file that have been deleted or modified. This can provide access to information that was not available without these extensions. However, this access is still subject to the same access checks to which it is normally subject.

## Index of Security Parameters

| Security parameter                                          | Section                                              |
| ----------------------------------------------------------- | ---------------------------------------------------- |
| Signing Key Protection                                      | [3.2.5.4](#Section_66aeb6701e484cb9a6517c70f355cd16) |
| Extended Security - GSS mechanism                           | [3.3.5.3](#Section_1f152df0a61d4e769af6da96fa783c02) |
| Extended Security - Maximal User and Guest Rights per Share | [3.3.5.4](#Section_8e1132db35514a439552326236eb2c67) |
| Authentication Expiration Time                              | [3.3.2.1](#Section_d3866f1fcada48848b68999ee2142568) |

# Appendix A: Product Behavior

The information in this specification is applicable to the following Microsoft products or supplemental software. References to product versions include released service packs.

- Windows 2000 operating system
- Windows XP operating system
- Windows Server 2003 operating system
- Windows Server 2003 R2 operating system
- Windows Vista operating system
- Windows Server 2008 operating system
- Windows 7 operating system
- Windows Server 2008 R2 operating system
- Windows 8 operating system
- Windows Server 2012 operating system
- Windows 8.1 operating system
- Windows Server 2012 R2 operating system
- Windows 10 operating system
- Windows Server 2016 operating system

Exceptions, if any, are noted below. If a service pack or Quick Fix Engineering (QFE) number appears with the product version, behavior changed in that service pack or QFE. The new behavior also applies to subsequent service packs of the product unless otherwise specified. If a product edition appears with the product version, behavior is different in that product edition.

Unless otherwise specified, any statement of optional behavior in this specification that is prescribed using the terms SHOULD or SHOULD NOT implies product behavior in accordance with the SHOULD or SHOULD NOT prescription. Unless otherwise specified, the term MAY implies that the product does not follow the prescription.

[&lt;1&gt; Section 1.8](#Appendix_A_Target_1): UNIX extensions are not supported by Windows-based SMB clients and servers. The CAP_UNIX capability bit and the Information Level range are reserved to allow third party implementers to collaborate on the definition of these extensions. The development of a common set of extensions has been informally supported by the Storage Networking Industry Association (SNIA). See [\[SNIA\]](http://go.microsoft.com/fwlink/?LinkId=90519) for SNIA specification on vendor-extension fields.

[&lt;2&gt; Section 2.1](#Appendix_A_Target_2): The Direct TCP transport can be used by Windows-based SMB clients and servers.

[&lt;3&gt; Section 2.1](#Appendix_A_Target_3): Windows-based clients and servers use TCP port 445 as the destination TCP port on the SMB server, the well-known port number assigned by IANA to Microsoft-DS.

[&lt;4&gt; Section 2.1](#Appendix_A_Target_4): Windows 7 and Windows Server 2008 R2 servers without \[MS11-048\] do not disconnect the connection if the SMB message size exceeds 0x1FFFF bytes.

[&lt;5&gt; Section 2.2](#Appendix_A_Target_5): When an error occurs, Windows-based SMB servers return an Error Response message unless specifically required to return data, as specified for named pipe read operations and certain I/O control code requests and other exceptions specified in [\[MS-CIFS\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-CIFS%5d.pdf#Section_d416ff7cc536406ea9514f04b2fd1d2b). Windows-based SMB clients expect that an SMB server returns an Error Response, unless otherwise specified. Windows implementations return data along with these error codes:

- STATUS_MORE_PROCESSING_REQUIRED on a session setup request
- STATUS_BUFFER_OVERFLOW for a read request, IOCTL request, and Query Info request
- STATUS_INVALID_PARAMETER or STATUS_INVALID_VIEW_SIZE for CopyChunk IOCTL request return data along with the header
- STATUS_STOPPED_ON_SYMLINK includes the symbolic link data
- STATUS_BUFFER_TOO_SMALL returns a ULONG containing the required size

[&lt;6&gt; Section 2.2.1.1.1](#Appendix_A_Target_6): This feature is unavailable in Windows 2000 and Windows XP. When enabled previous versions of files are accessible as read-only.

[&lt;7&gt; Section 2.2.1.2.2](#Appendix_A_Target_7): This value is not supported in Windows 2000.

[&lt;8&gt; Section 2.2.1.2.2](#Appendix_A_Target_8): This value is not supported in Windows 2000.

[&lt;9&gt; Section 2.2.1.2.2](#Appendix_A_Target_9): This value is not supported in Windows 2000.

[&lt;10&gt; Section 2.2.1.2.2](#Appendix_A_Target_10): This value is not supported in Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows XP, Windows Vista, or Windows Server 2008.

[&lt;11&gt; Section 2.2.1.2.2](#Appendix_A_Target_11): This value is not supported in Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows XP, Windows Vista, or Windows Server 2008.

[&lt;12&gt; Section 2.2.1.2.2](#Appendix_A_Target_12): This value is not supported in Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows XP, Windows Vista, or Windows Server 2008.

[&lt;13&gt; Section 2.2.1.2.2](#Appendix_A_Target_13): This value is not supported in Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows XP, Windows Vista, or Windows Server 2008.

[&lt;14&gt; Section 2.2.1.3.1](#Appendix_A_Target_14): Windows guarantees uniqueness of FileIds across a single volume.

[&lt;15&gt; Section 2.2.2.1](#Appendix_A_Target_15): If a client request contains an invalid command code, then Windows 2000 Server operating system and Windows XP server fail the requests by sending an error response with an NTSTATUS code of STATUS_SMB_BAD_COMMAND (ERRSRV/ERRbadcommand). Windows XP operating system Service Pack 1 (SP1), Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Vista, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 servers do not respond to such a request, and do not process further requests on that connection.

[&lt;16&gt; Section 2.2.2.2](#Appendix_A_Target_16): Windows-based clients and servers do not support NT_TRANSACT_CREATE2.

[&lt;17&gt; Section 2.2.2.3.1](#Appendix_A_Target_17): Windows 2000 Server does not support the SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO and SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO Information Levels.

[&lt;18&gt; Section 2.2.2.3.5](#Appendix_A_Target_18): Pass-through Information Levels on Windows-based servers map directly to native Windows NT operating system Information Classes, as specified in [\[MS-FSCC\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSCC%5d.pdf#Section_efbfe12773ad41409967ec6500e66d5e) sections 2.4 and 2.5. Windows-based servers do not support setting the following NT Information Levels via the pass-through Information Level mechanism.

| Information level          | Error code           |
| -------------------------- | -------------------- |
| FileLinkInformation        | STATUS_NOT_SUPPORTED |
| FileMoveClusterInformation | STATUS_NOT_SUPPORTED |
| FileTrackingInformation    | STATUS_NOT_SUPPORTED |
| FileCompletionInformation  | STATUS_NOT_SUPPORTED |
| FileMailslotSetInformation | STATUS_NOT_SUPPORTED |

All other Information Levels are passed through to the underlying object store or file system. Refer to \[MS-FSCC\] sections 2.4 and 2.5 for a further list of Information Levels that are not supported by Windows file systems and the error codes that can be returned.

[&lt;19&gt; Section 2.2.2.3.6](#Appendix_A_Target_19): These extensions, known as UNIX extensions, are not supported by Windows-based SMB clients and servers. The CAP_UNIX capability bit and the Information Level range specified are reserved to allow third party implementers to collaborate on the definition of these extensions. The development of a common set of extensions has been informally supported by the Storage Networking Industry Association (SNIA).

[&lt;20&gt; Section 2.2.2.4](#Appendix_A_Target_20): For a detailed listing of possible status codes available on Windows implementations, see [\[MS-ERREF\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-ERREF%5d.pdf#Section_1bc92ddfb79e413cbbaa99a5281a6c90). For a list of error codes used by the SMB Version 1.0 Protocol and CIFS Protocol, see \[MS-CIFS\] section 2.2.2.4.

[&lt;21&gt; Section 2.2.3.1](#Appendix_A_Target_21): Windows-based servers set the bits in the **Flags2** field with the same value(s) that were sent by the client in the request. Windows-based clients ignore this field when they receive the response.

[&lt;22&gt; Section 2.2.3.1](#Appendix_A_Target_22): Windows clients set this flag in all SMB requests if the client's configuration requires signing. This flag is not applicable to Windows 2000.

[&lt;23&gt; Section 2.2.3.1](#Appendix_A_Target_23): Windows-based SMB servers always ignore the SMB_FLAGS2_IS_LONG_NAME flag.

[&lt;24&gt; Section 2.2.4.1.2](#Appendix_A_Target_24): Windows-based servers support the notion of a guest account and set this field based on the defined guest account rights on the server.

[&lt;25&gt; Section 2.2.4.1.2](#Appendix_A_Target_25): Windows-based SMB servers set this field to an arbitrary value that is ignored on receipt. The servers do not send any data in this message.

[&lt;26&gt; Section 2.2.4.2.1](#Appendix_A_Target_26): Windows clients always set this field to 0xFFFFFFFF when reading from a Named Pipe or I/O device.

[&lt;27&gt; Section 2.2.4.2.1](#Appendix_A_Target_27): Windows-based servers support MaxCountHigh, but ignore it if set to 0xFFFF.

[&lt;28&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_28): Windows defaults to a **MaxBufferSize** value of 16,644 bytes on server versions of Windows. Windows defaults to a **MaxBufferSize** value of 4,356 bytes on client versions of Windows.

[&lt;29&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_29): Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 do not support SMB_COM_READ_RAW or SMB_COM_WRITE_RAW and disconnect the client by closing the underlying transport connection if either command is received from the client.

[&lt;30&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_30): Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 do not support SMB_COM_READ_MPX or SMB_COM_WRITE_MPX and disconnect the client by closing the underlying transport connection if either command is received from the client.

[&lt;31&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_31): Windows-based clients assume that CAP_NT_FIND is set if CAP_NT_SMBS is set.

[&lt;32&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_32): Windows-based clients and servers take advantage of CAP_INFOLEVEL_PASSTHRU, when available, to prevent the need to map from native file and directory information structures to comparable SMB structures.

[&lt;33&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_33): With CAP_LARGE_READX enabled, Windows-based servers provide a statically configured maximum read length, which defaults to 64 kilobytes. Windows-based clients and servers support CAP_LARGE_READX, which permits file transfers larger than the negotiated MaxBufferSize.

[&lt;34&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_34): Windows-based clients and servers support CAP_LARGE_WRITEX, which permits file transfers larger than the negotiated MaxBufferSize.

[&lt;35&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_35): Windows 2000 and Windows XP clients and servers support CAP_LWIO.

[&lt;36&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_36): Windows-based clients and servers do not support CAP_UNIX; therefore, this capability is never set.

[&lt;37&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_37): Windows-based clients and servers do not support CAP_COMPRESSED_DATA, and this capability is never set.

[&lt;38&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_38): Windows servers do not set the CAP_DYNAMIC_REAUTH flag, even if dynamic re-authentication is supported. On Windows XP, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016, all clients and servers support dynamic re-authentication.

[&lt;39&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_39): Windows-based clients and servers do not support CAP_PERSISTENT_HANDLES.

[&lt;40&gt; Section 2.2.4.5.2.1](#Appendix_A_Target_40): Windows-based clients use the **ServerGUID** field.

[&lt;41&gt; Section 2.2.4.5.2.2](#Appendix_A_Target_41): Windows-based servers default to a **MaxBufferSize** value of 16,644 bytes. Windows-based clients default to a **MaxBufferSize** value of 4,356 bytes.

[&lt;42&gt; Section 2.2.4.5.2.2](#Appendix_A_Target_42): Windows-based clients expect 8-byte cryptographic challenges. Windows-based servers provide 8-bit cryptographic challenges.

[&lt;43&gt; Section 2.2.4.6.1](#Appendix_A_Target_43): Windows-based servers only check for and store a small number of client capabilities:

- CAP_UNICODE
- CAP_LARGE_FILES
- CAP_NT_SMBS
- CAP_NT_FIND
- CAP_NT_STATUS
- CAP_EXTENDED_SECURITY
- CAP_LEVEL_II_OPLOCKS

Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 also check for CAP_DYNAMIC_REAUTH.

[&lt;44&gt; Section 2.2.4.6.1](#Appendix_A_Target_44): Windows-based SMB clients set this field based upon the version and service pack level of the Windows operating system. A list of possible values for this field includes the following.

| Windows OS version                                             | Native OS string                        |
| -------------------------------------------------------------- | --------------------------------------- |
| Windows 2000                                                   | Windows 5.0                             |
| Windows XP operating system Service Pack 2 (SP2)               | Windows 2002 Service Pack 2             |
| Windows Server 2003 operating system with Service Pack 2 (SP2) | Windows Server 2003 3790 Service Pack 2 |

Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, and Windows Server 2016 set this field to an empty string.

[&lt;45&gt; Section 2.2.4.6.1](#Appendix_A_Target_45): Windows-based SMB clients set this field based upon the version of the Windows operating system. A list of possible values for this field includes the following:

| Windows OS version  | NativeLanMan string      |
| ------------------- | ------------------------ |
| Windows 2000        | Windows 2000 LAN Manager |
| Windows XP SP2      | Windows 2002 5.1         |
| Windows Server 2003 | Windows Server 2003 5.2  |

Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, and Windows Server 2016 set this field to an empty string.

[&lt;46&gt; Section 2.2.4.7.2](#Appendix_A_Target_46): SMB clients on Windows XP, Windows Vista, Windows 7, Windows 8, Windows 8.1, and Windows 10 cache directory information if this bit is set on a share. SMB clients on all server versions of Windows do not cache directory information by default even if this bit is set on a share. Caching directory information by SMB clients on Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 can be enabled via a Windows registry setting. Windows 2000 operating system does not support directory caching.

[&lt;47&gt; Section 2.2.4.7.2](#Appendix_A_Target_47): Windows-based clients and servers support the notion of a guest account and set this field to the access allowed for the guest account.

[&lt;48&gt; Section 2.2.4.9.1](#Appendix_A_Target_48): Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 also support two new **CreateOptions** flags:

- FILE_OPEN_REQUIRING_OPLOCK (0x00010000). Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 ignore this bit if set in the request. All other Windows-based SMB servers fail requests with the FILE_OPEN_REQUIRING_OPLOCK option set, and return STATUS_INVALID_PARAMETER in the **Status** field of the SMB header in the server response.
- FILE_DISALLOW_EXCLUSIVE (0x00020000). Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 ignore this bit if it is set in the request. All other Windows-based SMB servers fail requests with this option set, and return STATUS_INVALID_PARAMETER in the **Status** field of the SMB header in the server response.

[&lt;49&gt; Section 2.2.4.9.2](#Appendix_A_Target_49): Windows-based SMB servers send 50 (0x32) words in the extended response although they set the **WordCount** field to 0x2A.

[&lt;50&gt; Section 2.2.4.9.2](#Appendix_A_Target_50): Windows-based servers set the **VolumeGUID** field to zero; otherwise, this field is uninitialized. The **VolumeGUID** field is ignored by Windows-based SMB clients.

[&lt;51&gt; Section 2.2.4.9.2](#Appendix_A_Target_51): Windows-based servers set the **FileId** field to zero. The **FileId** field is ignored by Windows-based SMB clients.

[&lt;52&gt; Section 2.2.4.9.2](#Appendix_A_Target_52): Windows-based servers and clients support the notion of a guest account.

[&lt;53&gt; Section 2.2.4.9.2](#Appendix_A_Target_53): Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 set this field to zero; otherwise, this field can be sent uninitialized.

[&lt;54&gt; Section 2.2.5.1](#Appendix_A_Target_54): Windows-based clients never send this request. Windows-based servers fail this request with STATUS_INVALID_PARAMETER.

[&lt;55&gt; Section 2.2.5.2](#Appendix_A_Target_55): Windows-based clients never send this request.

[&lt;56&gt; Section 2.2.6.1.1](#Appendix_A_Target_56): Windows-based clients do not issue TRANS2_FIND_FIRST2 requests with the special @GMT-\* pattern in the **FileName** field natively. Applications that run on Windows-based clients, however, are allowed to explicitly include the @GMT-\* pattern in the pathname that they supply.

[&lt;57&gt; Section 2.2.6.1.1](#Appendix_A_Target_57): Windows-based clients allow the @GMT-\* wildcard to be sent using Information Levels other than SMB_COM_FIND_FILE_BOTH_DIRECTORY_INFO.

[&lt;58&gt; Section 2.2.6.4](#Appendix_A_Target_58): Support for this subcommand was introduced in Windows 2000.

[&lt;59&gt; Section 2.2.7.1.2](#Appendix_A_Target_59): Windows-based servers set the **VolumeGUID** field to zero; otherwise, this field is uninitialized. The **VolumeGUID** field is ignored by Windows-based SMB clients.

[&lt;60&gt; Section 2.2.7.1.2](#Appendix_A_Target_60): Windows-based servers set the **FileId** field to zero. The **FileId** field is ignored by Windows-based SMB clients.

[&lt;61&gt; Section 2.2.7.1.2](#Appendix_A_Target_61): Windows-based servers and clients support guest accounts.

[&lt;62&gt; Section 2.2.7.2.1](#Appendix_A_Target_62): Only Windows Server 2003 operating system with Service Pack 1 (SP1), Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 support these new FSCTLs. All other Windows-based servers fail requests that contain these FSCTL codes with STATUS_NOT_SUPPORTED.

[&lt;63&gt; Section 2.2.7.2.1](#Appendix_A_Target_63): A definitive list of Windows FSCTL and IOCTL control codes and their structures (if any) is specified in \[MS-FSCC\] section 2.3.

[&lt;64&gt; Section 2.2.7.2.1](#Appendix_A_Target_64): Only Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 support this FSCTL. All other Windows-based servers fail the request with STATUS_NOT_SUPPORTED.

[&lt;65&gt; Section 2.2.7.2.1](#Appendix_A_Target_65): Only Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 servers support this FSCTL. All other Windows-based servers fail the request with STATUS_NOT_SUPPORTED.

[&lt;66&gt; Section 2.2.7.2.1.1](#Appendix_A_Target_66): Windows-based clients do not initialize the **Reserved** field to zero.

[&lt;67&gt; Section 2.2.7.2.2.2](#Appendix_A_Target_67): Windows-based servers set this field to an arbitrary number of uninitialized bytes.

[&lt;68&gt; Section 2.2.8.1.1](#Appendix_A_Target_68): Windows-based SMB servers set the **FileIndex** field to a nonzero value if the underlying object store supports indicating the position of a file within the parent directory.

[&lt;69&gt; Section 2.2.8.1.2](#Appendix_A_Target_69): The SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO Information Level is not present in Windows 2000 Server and Windows XP.

[&lt;70&gt; Section 2.2.8.1.2](#Appendix_A_Target_70): Windows-based SMB servers set the **FileIndex** field to a nonzero value if the underlying object store supports indicating the position of a file within the parent directory.

[&lt;71&gt; Section 2.2.8.1.2](#Appendix_A_Target_71): Windows-based servers set this field to an arbitrary value.

[&lt;72&gt; Section 2.2.8.1.3](#Appendix_A_Target_72): The SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO Information Level is not present in Windows 2000 Server and Windows XP.

[&lt;73&gt; Section 2.2.8.1.3](#Appendix_A_Target_73): Windows-based SMB servers set the **FileIndex** field to a nonzero value if the underlying object store supports indicating the position of a file within the parent directory.

[&lt;74&gt; Section 2.2.8.2.1](#Appendix_A_Target_74): The following attribute flags are removed by the Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 SMB server before sending the attribute data block to the client:

- FILE_SUPPORTS_TRANSACTIONS
- FILE_SUPPORTS_OPEN_BY_FILE_ID

[&lt;75&gt; Section 3.2.1.1](#Appendix_A_Target_75): Windows 2000 Server supports the _Disabled_ state.

[&lt;76&gt; Section 3.2.3](#Appendix_A_Target_76): **Client.SupportsExtendedSecurity** is TRUE for Windows-based clients.

[&lt;77&gt; Section 3.2.3](#Appendix_A_Target_77): Windows-based SMB clients on Windows 2000, Windows XP, and Windows Vista support 32-bit process IDs and use this field when sending the following SMB messages: SMB_COM_NT_CREATE_ANDX and SMB_COM_OPEN_PRINT_FILE. Windows-based SMB clients on Windows 2000, Windows XP, and Windows Vista also support and use this field when sending SMB_COM_TRANSACTION, SMB_COM_TRANSACTION2, and SMB_COM_TRANSACT messages when the server supports the CAP_NT_SMBS bit. The CAP_NT_SMBS bit is set in the Capabilities field in the SMB_COM_NEGOTIATE response (\[MS-CIFS\] section 2.2.4.52.2). Windows 7, Windows 8, Windows 8.1, and Windows 10 SMB clients do not support 32-bit process IDs and set this field to zero when sending SMB messages. Windows-based SMB servers support 32-bit process IDs when receiving SMB messages.

[&lt;78&gt; Section 3.2.4.1.1](#Appendix_A_Target_78): Windows XP, Windows Vista, Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 clients scan pathnames for previous version tokens and set the SMB_FLAGS2_REPARSE_PATH flag if a token is found.

[&lt;79&gt; Section 3.2.4.2.4](#Appendix_A_Target_79): Windows-based SMB clients use the same connection to a server for all authentications other than terminal services. **Connections** configured for terminal services use one connection per user.

[&lt;80&gt; Section 3.2.4.2.4](#Appendix_A_Target_80): In an [SMB_COM_SESSION_SETUP_ANDX request (section 2.2.4.6.1)](#Section_a00d03613544484596ab309b4bb7705d), Windows-based SMB clients initialize the **SMB_Header.SecurityFeatures** field to 'BSRSPYL' (0x42 0x53 0x52 0x53 0x50 0x59 0x4C). Windows-based SMB servers ignore this value.

[&lt;81&gt; Section 3.2.4.2.4](#Appendix_A_Target_81): Windows-based clients implement this option.

[&lt;82&gt; Section 3.2.4.2.4](#Appendix_A_Target_82):

- Windows-based clients support extended security.
- Windows systems implement the first option that is previously described.

[&lt;83&gt; Section 3.2.4.2.4.1](#Appendix_A_Target_83): Windows-based SMB clients are configured by default to not send plain text passwords. Sending plain text passwords can be configured via a registry setting.

[&lt;84&gt; Section 3.2.4.2.5](#Appendix_A_Target_84): Windows 2000 client does not request **Client.Session.SessionKey** protection.

[&lt;85&gt; Section 3.2.4.3](#Appendix_A_Target_85): Windows-based clients issue an SMB_COM_NT_CREATE_ANDX request for the NT LM 0.12 dialect for which all of the extensions here are described.

[&lt;86&gt; Section 3.2.4.3.2](#Appendix_A_Target_86): Windows-based clients do not use this flag.

[&lt;87&gt; Section 3.2.4.4](#Appendix_A_Target_87): Windows-based clients set the **Timeout** field to 0xFFFFFFFF on pipe reads.

[&lt;88&gt; Section 3.2.4.4.1](#Appendix_A_Target_88): Windows-based clients issue large reads if the server supports them.

[&lt;89&gt; Section 3.2.4.6](#Appendix_A_Target_89): Windows-based clients send these requests to the server regardless of the Information Level provided in the request.

[&lt;90&gt; Section 3.2.4.11.1](#Appendix_A_Target_90): Windows XP, Windows Vista, Windows 7, Windows 8, Windows 8.1, and Windows 10 clients use this FSCTL. Windows 2000-based clients can use this FSCTL if the previous versions' down-level application is installed on them.

[&lt;91&gt; Section 3.2.4.12](#Appendix_A_Target_91): Windows XP operating system Service Pack 3 (SP3), Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 clients support DFS.

[&lt;92&gt; Section 3.2.5.2](#Appendix_A_Target_92): Windows-based SMB servers support Extended Security. They all are configured to use SPNEGO, as specified in [\[RFC4178\]](http://go.microsoft.com/fwlink/?LinkId=90461), as their GSS authentication protocol. Windows operating systems that use extended security send a GSS token (or fragment) if their SPNEGO implementation supports it. See \[RFC4178\] for details on Windows behavior.

[&lt;93&gt; Section 3.2.5.2](#Appendix_A_Target_93): When the server completes negotiation and returns the CAP_EXTENDED_SECURITY flag as not set, Windows-based SMB clients query the [**Key Distribution Center (KDC)**](#gt_6e5aafba-6b66-4fdd-872e-844f142af287) to verify whether a service ticket is registered for the given [**security principal name (SPN)**](#gt_2f43ba25-67d2-491e-9282-8ee83d855397). If the query indicates that the [**SPN**](#gt_547217ca-134f-4b43-b375-f5bca4c16ce4) is registered with the KDC, then the SMB client terminates the connection and returns an implementation-specific security downgrade error to the caller.

[&lt;94&gt; Section 3.2.5.3](#Appendix_A_Target_94): The Windows GSS implementation supports raw Kerberos / NTLM messages in the **SecurityBlob** as described in [\[MS-AUTHSOD\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-AUTHSOD%5d.pdf#Section_953d700a57cb4cf7b0c3a64f34581cc9) section 2.1.2.2.

[&lt;95&gt; Section 3.2.5.3](#Appendix_A_Target_95): Windows Vista operating system with Service Pack 1 (SP1), Windows Server 2008, Windows 7, Windows Server 2008 R2 operating system, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 servers fail a non-extended security session setup request with STATUS_INVALID_PARAMETER if the registry key is either missing or set to zero.

[&lt;96&gt; Section 3.3.2.1](#Appendix_A_Target_96): Windows-based servers implement this timer with a default value of 300 seconds.

[&lt;97&gt; Section 3.3.3](#Appendix_A_Target_97): **SupportsExtendedSecurity** is TRUE for Windows-based clients.

[&lt;98&gt; Section 3.3.3](#Appendix_A_Target_98): Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 set this value to 256.

[&lt;99&gt; Section 3.3.3](#Appendix_A_Target_99): Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 set this value to 1 megabyte.

[&lt;100&gt; Section 3.3.3](#Appendix_A_Target_100): Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 set this value to 16 megabytes.

[&lt;101&gt; Section 3.3.3](#Appendix_A_Target_101): Windows Server 2003 with SP1, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 set this value to 25 seconds.

[&lt;102&gt; Section 3.3.4.1.1](#Appendix_A_Target_102): Windows servers do not respond with an OS/2 error on the wire even if SMB_FLAGS2_NT_STATUS is set in the client request (see \[MS-CIFS\] section 2.2.3.1). If the negotiated dialect is DOS LANMAN 2.0, DOS LANMAN 2.1, or prior to LANMAN 1.0, an ERROR_GEN_FAILURE error is returned. Otherwise, the following table lists the corresponding DOS error (see \[MS-CIFS\] section 2.2.2.4 SMB Error Classes and Codes) that is returned:

| OS/2 Error                            | DOS Error                        |
| ------------------------------------- | -------------------------------- |
| STATUS_OS2_INVALID_LEVEL              | ERRunknownlevel                  |
| STATUS_OS2_EA_LIST_INCONSISTENT       | ERRbadealist                     |
| STATUS_OS2_NEGATIVE_SEEK              | ERRinvalidseek                   |
| STATUS_OS2_NO_MORE_SIDS               | ERROR_NO_MORE_SEARCH_HANDLES     |
| STATUS_OS2_EAS_DIDNT_FIT              | ERROR_EAS_DIDNT_FIT              |
| STATUS_OS2_EA_ACCESS_DENIED           | ERROR_EA_ACCESS_DENIED           |
| STATUS_OS2_CANCEL_VIOLATION           | ERROR_CANCEL_VIOLATION           |
| STATUS_OS2_ATOMIC_LOCKS_NOT_SUPPORTED | ERROR_ATOMIC_LOCKS_NOT_SUPPORTED |
| STATUS_OS2_CANNOT_COPY                | ERROR_CANNOT_COPY                |

[&lt;103&gt; Section 3.3.5.1](#Appendix_A_Target_103): Windows XP, Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, and Windows Server 2008 R2 fail a TREE_CONNECT_ANDX request to a share that does not allow anonymous access with STATUS_ACCESS_DENIED. All other requests, which require an access check (such as opening a file), are failed with STATUS_INVALID_HANDLE.

[&lt;104&gt; Section 3.3.5.1](#Appendix_A_Target_104): Windows XP, Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, and Windows Server 2008 R2 will fail the request with STATUS_ACCESS_DENIED.

[&lt;105&gt; Section 3.3.5.1](#Appendix_A_Target_105): Windows XP, Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, and Windows Server 2008 R2 will fail the request with STATUS_ACCESS_DENIED.

[&lt;106&gt; Section 3.3.5.1.1](#Appendix_A_Target_106): SMB servers on Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, and Windows Server 2012 support the SMB_FLAGS2_REPARSE_PATH flag and previous version access. An SMB server on Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, or Windows Server 2016 parses paths when the flag is not set but only when configured to do so. This flag is used to expose the previous version logic to applications that run on clients whose SMB client does not understand the SMB_FLAGS2_REPARSE_PATH flag and does not set it.

[&lt;107&gt; Section 3.3.5.1.2](#Appendix_A_Target_107): Windows servers grant level II oplocks, even if the client does not request an oplock.

[&lt;108&gt; Section 3.3.5.2](#Appendix_A_Target_108): Windows-based SMB servers support Extended Security, and are configured to use SPNEGO (as specified in \[RFC4178\]) as their GSS authentication protocol. Windows operating systems that use extended security send a GSS token (or fragment) if their SPNEGO implementation supports it. For details on Windows behavior, see \[RFC4178\].

[&lt;109&gt; Section 3.3.5.3](#Appendix_A_Target_109): Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, and Windows Server 2016 fail the [SMB_COM_SESSION_SETUP_ANDX request](#Section_1f152df0a61d4e769af6da96fa783c02) with STATUS_ACCESS_DENIED if both the EncryptData and RejectUnencryptedAccess registry keys are set to nonzero values.

[&lt;110&gt; Section 3.3.5.3](#Appendix_A_Target_110): The Windows GSS implementation supports raw Kerberos / NTLM messages in the **SecurityBlob** as described in \[MS-AUTHSOD\] section 2.1.2.2. If the client sends a zero length **SecurityBlob** in the request, the server-initiated SPNEGO exchange will be used.

[&lt;111&gt; Section 3.3.5.3](#Appendix_A_Target_111): NTLM authentication has no expiration time, so authentications done with NTLM do not expire. For the Windows implementation of Kerberos expiration time, see [\[MS-KILE\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-KILE%5d.pdf#Section_2a32282edd484ad9a542609804b02cc9) section 3.3.1.

[&lt;112&gt; Section 3.3.5.4](#Appendix_A_Target_112): Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 fail the [SMB_COM_TREE_CONNECT_ANDX request](#Section_8e1132db35514a439552326236eb2c67) with STATUS_ACCESS_DENIED, if **Share.ShareFlags** contains SHI1005_FLAGS_ENCRYPT_DATA and the **RejectUnencryptedAccess** registry key is set to a nonzero value.

[&lt;113&gt; Section 3.3.5.4](#Appendix_A_Target_113): Windows 2000 never sets the SMB_UNIQUE_FILE_NAME bit in the **OptionalSupport** field.

Windows XP sets the SMB_UNIQUE_FILE_NAME bit in the **OptionalSupport** field only if short file name generation is disabled by setting the NtfsDisable8dot3NameCreation registry key to 1; see [\[MSKB-121007\]](http://go.microsoft.com/fwlink/?LinkId=228457).

Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 also set the SMB_UNIQUE_FILE_NAME bit in the **OptionalSupport** field if the NoAliasingOnFilesystem registry key is set to 1 (enabled).

[&lt;114&gt; Section 3.3.5.4](#Appendix_A_Target_114): Windows 2000, Windows Server 2003, and Windows Server 2003 R2 set **GuestMaximalAccessRights** to access rights granted for null session. Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 set **GuestMaximalAccessRights** to zero.

[&lt;115&gt; Section 3.3.5.5](#Appendix_A_Target_115): Windows servers open or create files in the object store as described in [\[MS-FSA\]](file:///E:\Target\Windows\Published\Books\MS-SMB%5bMS-FSA%5d.pdf#Section_860b1516c45247b4bdbc625d344e2041) section 2.1.5.1 Server Requests an Open of a File, with the following mapping of input elements:

- **RootOpen** is provided in one of two ways:
  - If the **SMB_Parameters.Words.RootDirectoryFID** field is zero, **RootOpen** is provided by using the **SMB_Header.TID** field to find the matching **Server.TreeConnect** in the **Server.Connection.TreeConnectTable**. The server then acquires an **Open** on the **Server.TreeConnect.Share.LocalPath**, which is passed as **RootOpen**.
  - If the **SMB_Parameters.Words.RootDirectoryFID** field is non-zero, **RootOpen** is provided by looking up the **RootDirectoryFID** field in the **Server.Connection.FileOpenTable**.
- **PathName** is the **SMB_Data.Bytes.FileName** field of the request.
- **SecurityContext** is found by using the **SMB_Header.UID** field to look up the matching **Session** entry in the **Server.Connection.SessionTable**. The **Server.Session.UserSecurityContext** is passed as **SecurityContext**.
- **UserCertificate** is the certificate returned by the User-Certificate binding obtained during request processing.
- **DesiredAccess** is the **SMB_Parameters.Words.DesiredAccess** field of the request. The FILE_READ_ATTRIBUTES option is added (using a bitwise OR) to the set provided by the client. If the FILE_NO_INTERMEDIATE_BUFFERING flag is set, it is cleared, and FILE_WRITE_THROUGH is set.
- **ShareAccess** is the **SMB_Parameters.Words.ShareAccess** field of the request.
- **CreateOptions** is the **SMB_Parameters.Words.CreateOptions** field of the request. The FILE_COMPLETE_IF_OPLOCKED option is added (using a bitwise OR) to the set provided by the client. If the FILE_NO_INTERMEDIATE_BUFFERING flag is set, it is cleared, and FILE_WRITE_THROUGH is set.
- **CreateDisposition** is the **SMB_Parameters.Words.CreateDisposition** field of the request.
- **DesiredFileAttributes** is the **SMB_Parameters.Words.ExtFileAttributes** field of the request.
- **IsCaseSensitive** is set to FALSE if the SMB_FLAGS_CASE_INSENSITIVE bit is set in the **SMB_Header.Flags** field of the request. Otherwise, **IsCaseSensitive** is set depending upon system defaults.
- **OpLockKey** is empty.

The returned **Status** is copied into the **SMB_Header.Status** field of the response. If the operation fails, the **Status** is returned in an Error Response, and processing is complete.

If the operation is successful, processing continues as follows:

- If either the NT_CREATE_REQUEST_OPLOCK or the NT_CREATE_REQUEST_OPBATCH flag is set in the **SMB_Parameters.Words.Flags** field of the request, an OpLock is requested. Windows servers obtain OpLocks as described in \[MS-FSA\], section 3.1.5.17 Server Requests an Oplock, with the following mapping of input elements:
  - **Open** is the **Open** passed through from the preceding operation.
  - **Type** is LEVEL_BATCH if the NT_CREATE_REQUEST_OPBATCH flag is set, or LEVEL_ONE if the NT_CREATE_REQUEST_OPLOCK flag is set.

If an OpLock is granted, the **SMB_Parameters.Words.OpLockLevel** field of the response is set.

- Windows servers obtain the extended file attribute and timestamp response information by querying file information from the [**object store**](#gt_0cc469e1-4b1f-41c4-9f94-d6209fcf468e) as described in \[MS-FSA\], section 3.1.5.11 Server Requests a Query of File Information, with the following mapping of input elements:
  - **Open** is the **Open** passed through from the preceding operations.
  - **FileInformationClass** is **FileBasicInformation** (\[MS-FSCC\] section 2.4.7).

If the query fails, the **Status** is returned in an Error Response, and processing is complete. Otherwise:

- - **SMB_Parameters.Words.ExtFileAttributes** is set to **OutputBuffer.FileAttributes**.
    - **SMB_Parameters.Words.CreateTime** is set to **OutputBuffer.CreateTime**.
    - **SMB_Parameters.Words.LastAccessTime** is set to **OutputBuffer.LastAccessTime**.
    - **SMB_Parameters.Words.LastWriteTime** is set to **OutputBuffer.LastWriteTime**.
    - **SMB_Parameters.Words.LastChangeTime** is set to **OutputBuffer.ChangeTime**.
- Windows servers obtain the file size response field values by querying file information from the object store as described in \[MS-FSA\], section 3.1.5.11 Server Requests a Query of File Information, with the following mapping of input elements:
  - **Open** is the **Open** passed through from the preceding operations.
  - **FileInformationClass** is **FileStandardInformation** (\[MS-FSCC\] section 2.4.38).

If the query fails, the **Status** is returned in an Error Response, and processing is complete. Otherwise:

- - **SMB_Parameters.Words.AllocationSize** is set to **OutputBuffer.AllocationSize**.
    - **SMB_Parameters.Words.EndOfFile** is set to **OutputBuffer.EndOfFile**.

If the query fails, the **Status** is returned in an Error Response, and processing is complete.

- **Open.File.FileType** is used to set the **SMB_Parameters.Words.ResourceType** and **SMB_Parameters.Words.Directory** fields of the response.
- A new **FID** is generated for the **Open** returned. All of the other results of the **Open** operation are ignored. The **FID** is copied into the **SMB_Parameters.Words.FID** field of the response.

[&lt;116&gt; Section 3.3.5.5](#Appendix_A_Target_116): Windows 2000, Windows XP, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, and Windows Server 2012 do not perform this verification.

[&lt;117&gt; Section 3.3.5.5](#Appendix_A_Target_117): When the client sends a batched request that begins with an SMB_COM_NT_CREATE_ANDX request with the NT_CREATE_REQUEST_EXTENDED_RESPONSE bit set in the **Flags** field, Windows-based servers return the DOS error code ERRSRV/ERRerror and return an extended response only for the SMB_COM_NT_CREATE_ANDX request.

[&lt;118&gt; Section 3.3.5.5](#Appendix_A_Target_118): Windows-based servers set the **FileStatusFlags** using the following mapping of output elements specified in \[MS-FSA\] section 2.1.5.1:

- NO_EAS is set if the returned **Open.File.ExtendedAttributesLength** is zero, otherwise it is not set.
- NO_SUBSTREAMS is set if the returned **Open.File.StreamList** is less than or equal to one, otherwise it is not set.
- NO_REPARSETAG is set if the returned Open.File.ReparseTag is empty, otherwise it is not set.

[&lt;119&gt; Section 3.3.5.5](#Appendix_A_Target_119): **[NTFS](#gt_86f79a17-c0be-4937-8660-0cf6ce5ddc1a)** supports streams. [**FAT**](#gt_f2bf797b-e733-4fb9-b5e5-7e122f4abbe0) and FAT32 file systems do not support streams.

[&lt;120&gt; Section 3.3.5.5](#Appendix_A_Target_120): SMB servers on Windows 2000 Server, Windows Server 2003, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 return zero for the **VolumeGUID** and **FileId**. All other Windows-based servers set the **VolumeGUID** and **FileId** fields using the following mapping of output elements, specified in \[MS-FSA\] section 2.1.5.1:

- **VolumeGUID** is set to the returned **Open.File.Volume.VolumeId**.
- **FileId** is set to the returned **Open.File.FileId**.

[&lt;121&gt; Section 3.3.5.5](#Appendix_A_Target_121): Windows-based servers set the **MaximalAccessRights** and **GuestMaximalAccessRights** fields using the following mapping of output elements, specified in \[MS-FSA\] section 2.1.5.1:

- **MaximalAccessRights** is set to the returned **Open.GrantedAcess**.
- Windows 2000, Windows Server 2003, and Windows Server 2003 R2 set **GuestMaximalAccessRights** to access rights granted for null session.
- Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 set **GuestMaximalAccessRights** to zero.

[&lt;122&gt; Section 3.3.5.6](#Appendix_A_Target_122): Windows servers open existing files in the object store as described in \[MS-FSA\] section 2.1.5.1 Server Requests an Open of a File, with the following mapping of input elements:

- **RootOpen** is provided by using the **SMB_Header.TID** to find the matching **Server.TreeConnect** in the **Server.Connection.TreeConnectTable**. The server then acquires an **Open** on **Server.TreeConnect.Share.LocalPath**, which is passed as **RootOpen**.
- **PathName** is the **SMB_Data.Bytes.FileName** field from the request.
- **SecurityContext** is found by using the **SMB_Header.UID** to look up the matching **Session** entry in the **Server.Connection.SessionTable**. The **Server.Session.UserSecurityContext** is passed as **SecurityContext**.
- **UserCertificate** is the certificate returned by the User-Certificate binding obtained during request processing.
- **DesiredAccess** is set as follows:
  - The **AccessMode** subfield of the **AccessMode** field in the request is used to set the value of **DesiredAccess**. The **AccessMode** subfield represents the lowest-order four bits of the **AccessMode** field (0x0007), as shown in the table in \[MS-CIFS\] section 2.2.4.3.1. The mapping of values is as follows:

| AccessMode.AccessMode | DesiredAccess                                                  |
| --------------------- | -------------------------------------------------------------- |
| 0                     | GENERIC_READ 0x80000000                                        |
| 1                     | GENERIC_WRITE \| FILE_READ_ATTRIBUTES 0x40000000 \| 0x00000080 |
| 2                     | GENERIC_READ \| GENERIC_WRITE 0x80000000 \| 0x40000000         |
| 3                     | GENERIC_READ \| GENERIC_EXECUTE 0x80000000 \| 0x20000000       |

For any other value of **AccessMode.AccessMode**, this algorithm returns STATUS_OS2_INVALID_ACCESS (ERRDOS/ERRbadaccess).

- **ShareAccess** is set as follows:
  - The **SharingMode** subfield of the **AccessMode** field in the request is used to set the value of **ShareAccess**. The **SharingMode** subfield is a 4-bit subfield of the **AccessMode** field (0x0070), as shown in the table in \[MS-CIFS\] section 2.2.4.3.1. The mapping of values is as follows:

| AccessMode.SharingMode | ShareAccess                         |
| ---------------------- | ----------------------------------- |
| 0                      | Compatibility mode (see below)      |
| 1                      | 0x0L (don't share, exclusive use)   |
| 2                      | FILE_SHARE_READ                     |
| 3                      | FILE_SHARE_WRITE                    |
| 4                      | FILE_SHARE_READ \| FILE_SHARE_WRITE |
| 0xFF                   | FCB mode (see below)                |

- - For Compatibility mode, special filename suffixes (after the '.' in the filename) are mapped to **SharingMode** 4. The special filename suffix set is: "EXE", "DLL", "SYM", and "COM". All other file names are mapped to **SharingMode** 3.
    - For FCB mode, if the file is already open on the server, the current sharing mode of the existing Open is preserved, and a **FID** for the file is returned. If the file is not already open on the server, the server attempts to open the file using **SharingMode** 1.
    - For any other value of **AccessMode.SharingMode**, this algorithm returns STATUS_OS2_INVALID_ACCESS (ERRDOS/ERRbadaccess).
- **CreateOptions** bits are set as follows:

| CreateOptions value            | SMB_COM_OPEN_ANDX equivalent                                           |
| ------------------------------ | ---------------------------------------------------------------------- |
| FILE_WRITE_THROUGH             | AccessMode.WritethroughMode == 1                                       |
| FILE_SEQUENTIAL_ONLY           | AccessMode.ReferenceLocality == 1                                      |
| FILE_RANDOM_ACCESS             | AccessMode.ReferenceLocality == 2 or AccessMode.ReferenceLocality == 3 |
| FILE_NO_INTERMEDIATE_BUFFERING | AccessMode.CacheMode == 1                                              |
| FILE_NON_DIRECTORY_FILE        | Is set                                                                 |
| FILE_COMPLETE_IF_OPLOCKED      | Is set                                                                 |
| FILE_NO_EA_KNOWLEDGE           | SMB_Header.Flags2.SMB_FLAGS2_KNOWS_EAS == 0                            |

- - All other bits are unused.
- **CreateDisposition** is set as follows:

| CreateDisposition value                                                     | SMB_Parameters.Word.OpenMode equivalent |
| --------------------------------------------------------------------------- | --------------------------------------- |
| Invalid combination; return STATUS_OS2_INVALID_ACCESS (ERRDOS/ERRbadaccess) | FileExistsOpts = 0 & CreateFile = 0     |
| FILE_CREATE                                                                 | FileExistsOpts = 0 & CreateFile = 1     |
| FILE_OPEN                                                                   | FileExistsOpts = 1 & CreateFile = 0     |
| FILE_OPEN_IF                                                                | FileExistsOpts = 1 & CreateFile = 1     |
| FILE_OVERWRITE                                                              | FileExistsOpts = 2 & CreateFile = 0     |
| FILE_OVERWRITE_IF                                                           | FileExistsOpts = 2 & CreateFile = 1     |

[&lt;123&gt; Section 3.3.5.6](#Appendix_A_Target_123): Windows-based servers set the **MaximalAccessRights** and **GuestMaximalAccessRights** fields using the following mapping of output elements specified in \[MS-FSA\] section 2.1.5.1:

- **MaximalAccessRights** is set to the returned **Open.GrantedAcess**.
- Windows 2000, Windows Server 2003, and Windows Server 2003 R2 set **GuestMaximalAccessRights** to access rights granted for null session.
- Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 set **GuestMaximalAccessRights** to zero.

[&lt;124&gt; Section 3.3.5.7](#Appendix_A_Target_124): Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 SMB servers fail the SMB_COM_READ_ANDX request with STATUS_INVALID_SMB if it is compounded with an SMB_COM_CLOSE request.

[&lt;125&gt; Section 3.3.5.7](#Appendix_A_Target_125): If the read operation is on a file and the count of bytes to read is greater than or equal to 0x00010000 (64K), Windows SMB servers set **DataLength** and **DataLengthHigh** fields to 0 and do not return any data but return STATUS_SUCCESS.

[&lt;126&gt; Section 3.3.5.8](#Appendix_A_Target_126): Windows-based servers ignore the **ByteCount** field, and calculate the number of bytes to be written as **DataLength** | **DataLengthHigh** <<16.

[&lt;127&gt; Section 3.3.5.9](#Appendix_A_Target_127): Windows 2000 Server and Windows Server 2003 return STATUS_NO_MORE_FILES if the **FileName** field of the SMB_COM_SEARCH request is an empty string.

[&lt;128&gt; Section 3.3.5.10.1](#Appendix_A_Target_128): Windows behavior for each Information Class is specified in each Information Class' corresponding subsection of either \[MS-FSA\] sections 2.1.5.11 or 2.1.5.12.

[&lt;129&gt; Section 3.3.5.10.2](#Appendix_A_Target_129): Windows servers support these new Information Levels for directory queries.

[&lt;130&gt; Section 3.3.5.10.2](#Appendix_A_Target_130): Windows Server 2003, Windows Server 2003 R2, Windows Server 2008, Windows Server 2008 R2, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 support previous versions but do not support this method of enumerating them, by default. This feature can be configured to be active by the administrator. The purpose is to allow an application (on a client that does not support the IOCTL command) to have a method of enumerating the previous versions.

[&lt;131&gt; Section 3.3.5.10.6](#Appendix_A_Target_131): If the requested Information Class is FileRenameInformation, then the following validation is performed:

- If **RootDirectory** is not NULL, then the request fails with STATUS_INVALID_PARAMETER.
- If the file name pointed to by the _FileName_ parameter of the FILE_RENAME_INFORMATION structure contains a separator character, then the request fails with STATUS_NOT_SUPPORTED.

If the server file system does not support this **Information Level**, then it fails the request with STATUS_OS2_INVALID_LEVEL. Otherwise, it attempts to apply the attributes to the target file and return the success or failure code in the response.

[&lt;132&gt; Section 3.3.5.10.7](#Appendix_A_Target_132): Windows 2000 Server, Windows Server 2003, Windows Server 2003 R2, and Windows Server 2008 do not break a batch oplock when processing a TRANS2_SET_PATH_INFORMATION request. Windows Server 2008 R2, Windows Server 2012, Windows Server 2012 R2, and Windows Server 2016 break a batch oplock when processing the request.

[&lt;133&gt; Section 3.3.5.11.1](#Appendix_A_Target_133): Windows 2000, Windows XP, Windows Server 2003, Windows Server 2003 R2, Windows Vista, Windows Server 2008, Windows 7, Windows Server 2008 R2, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 SMB servers pass IOCTL requests through to the underlying object store.

[&lt;134&gt; Section 3.3.5.11.1](#Appendix_A_Target_134): The server blocks certain FSCTL requests by not passing them through to the underlying file system for processing. The following FSCTLs are explicitly blocked by the server and are failed with STATUS_NOT_SUPPORTED.

| Name                            | Value      |
| ------------------------------- | ---------- |
| FSCTL_REQUEST_OPLOCK_LEVEL_1    | 0x00090000 |
| FSCTL_REQUEST_OPLOCK_LEVEL_2    | 0x00090004 |
| FSCTL_REQUEST_BATCH_OPLOCK      | 0x00090008 |
| FSCTL_OPLOCK_BREAK_ACKNOWLEDGE  | 0x0009000C |
| FSCTL_OPBATCH_ACK_CLOSE_PENDING | 0x00090010 |
| FSCTL_OPLOCK_BREAK_NOTIFY       | 0x00090014 |
| FSCTL_MOVE_FILE                 | 0x00090074 |
| FSCTL_MARK_HANDLE               | 0x000900FC |
| FSCTL_QUERY_RETRIEVAL_POINTERS  | 0x0009003B |
| FSCTL_PIPE_ASSIGN_EVENT         | 0x00110000 |
| FSCTL_GET_VOLUME_BITMAP         | 0x0009006F |
| FSCTL_GET_NTFS_FILE_RECORD      | 0x00090068 |
| FSCTL_INVALIDATE_VOLUMES        | 0x00090054 |

Windows does not support USN journal calls because they require a volume handle. The following USN journal calls are also failed with STATUS_NOT_SUPPORTED.

| Name                     | Value      |
| ------------------------ | ---------- |
| FSCTL_READ_USN_JOURNAL   | 0x000900BB |
| FSCTL_CREATE_USN_JOURNAL | 0x000900E7 |
| FSCTL_QUERY_USN_JOURNAL  | 0x000900F4 |
| FSCTL_DELETE_USN_JOURNAL | 0x000900F8 |
| FSCTL_ENUM_USN_DATA      | 0x000900B3 |

The following FSCTLs are explicitly blocked by Windows Server 2008 R2, Windows Server 2012, and Windows Server 2012 R2 and are not passed through to the object store. They are failed with STATUS_NOT_SUPPORTED.

| Name                                 | Value      |
| ------------------------------------ | ---------- |
| FSCTL_REQUEST_OPLOCK_LEVEL_1         | 0x00090000 |
| FSCTL_REQUEST_OPLOCK_LEVEL_2         | 0x00090004 |
| FSCTL_REQUEST_BATCH_OPLOCK           | 0x00090008 |
| FSCTL_REQUEST_FILTER_OPLOCK          | 0x0009005C |
| FSCTL_OPLOCK_BREAK_ACKNOWLEDGE       | 0x0009000C |
| FSCTL_OPBATCH_ACK_CLOSE_PENDING      | 0x00090010 |
| FSCTL_OPLOCK_BREAK_NOTIFY            | 0x00090014 |
| FSCTL_MOVE_FILE                      | 0x00090074 |
| FSCTL_MARK_HANDLE                    | 0x000900FC |
| FSCTL_QUERY_RETRIEVAL_POINTERS       | 0x0009003B |
| FSCTL_PIPE_ASSIGN_EVENT              | 0x00110000 |
| FSCTL_GET_VOLUME_BITMAP              | 0x0009006F |
| FSCTL_GET_NTFS_FILE_RECORD           | 0x00090068 |
| FSCTL_INVALIDATE_VOLUMES             | 0x00090054 |
| FSCTL_READ_USN_JOURNAL               | 0x000900BB |
| FSCTL_CREATE_USN_JOURNAL             | 0x000900E7 |
| FSCTL_QUERY_USN_JOURNAL              | 0x000900F4 |
| FSCTL_DELETE_USN_JOURNAL             | 0x000900F8 |
| FSCTL_ENUM_USN_DATA                  | 0x000900B3 |
| FSCTL_QUERY_DEPENDENT_VOLUME         | 0x000901F0 |
| FSCTL_SD_GLOBAL_CHANGE               | 0x000901F4 |
| FSCTL_GET_BOOT_AREA_INFO             | 0x00090230 |
| FSCTL_GET_RETRIEVAL_POINTER_BASE     | 0x00090234 |
| FSCTL_SET_PERSISTENT_VOLUME_STATE    | 0x00090238 |
| FSCTL_QUERY_PERSISTENT_VOLUME_STATE  | 0x0009023C |
| FSCTL_REQUEST_OPLOCK                 | 0x00090240 |
| FSCTL_TXFS_MODIFY_RM                 | 0x00098144 |
| FSCTL_TXFS_QUERY_RM_INFORMATION      | 0x00094148 |
| FSCTL_TXFS_ROLLFORW ARD_REDO         | 0x00098150 |
| FSCTL_TXFS_ROLLFORWARD_UNDO          | 0x00098154 |
| FSCTL_TXFS_START_RM                  | 0x00098158 |
| FSCTL_TXFS_SHUTDOWN_RM               | 0x0009815C |
| FSCTL_TXFS_READ_BACKUP_INFORMATION   | 0x00094160 |
| FSCTL_TXFS_WRITE_BACKUP_INFORMATION  | 0x00098164 |
| FSCTL_TXFS_CREATE_SECONDARY_RM       | 0x00098168 |
| FSCTL_TXFS_GET_METADATA_INFO         | 0x0009416C |
| FSCTL_TXFS_GET_TRANSACTED_VERSION    | 0x00094170 |
| FSCTL_TXFS_SAVEPOINT_INFORMATION     | 0x00098178 |
| FSCTL_TXFS_CREATE_MINIVERSION        | 0x0009817C |
| FSCTL_TXFS_TRANSACTION_ACTIVE        | 0x0009418C |
| FSCTL_TXFS_LIST_TRANSACTIONS         | 0x000941E4 |
| FSCTL_TXFS_READ_BACKUP_INFORMATION2  | 0x000901F8 |
| FSCTL_TXFS_WRITE_BACKUP_INFORMATION2 | 0x00090200 |

The following FSCTL is explicitly blocked by Windows 8 and Windows Server 2012 and is failed with STATUS_NOT_SUPPORTED.

| Name                         | Value      |
| ---------------------------- | ---------- |
| FSCTL_GET_RETRIEVAL_POINTERS | 0x00090073 |

[&lt;135&gt; Section 3.3.5.11.1.1](#Appendix_A_Target_135): When the **NumberOfSnapShotsReturned** field is zero, Windows-based SMB servers incorrectly append 2 zeroed bytes after NT_Trans_Data in the NT_TRANSACT_IOCTL response buffer of the FSCTL_SRV_ENUMERATE_SNAPSHOTS response.

[&lt;136&gt; Section 3.3.5.11.2](#Appendix_A_Target_136): Windows-based servers request quota information from the object store, as specified in \[MS-FSA\] section 2.1.5.11.24 if **Server.Open** is a file. If **Server.Open** is on a directory, then the processing follows with the following mapping of input elements:

- **Open** is an Open of **Server.Open.TreeConnect.Share.LocalPath** for the **Server.Open** indicated by the **SMB_Parameters.Words.FID** field of the request.
- **OutputBufferSize** is the **SMB_Parameters.Words.MaxDataCount** field of the request.
- **ReturnSingle** is the **NT_Trans_Parameters.ReturnSingleEntry** field of the request.
- **RestartScan** is the **NT_Trans_Parameters.RestartScan** field of the request.
- **SidList** is the **NT_Trans_Data.SidList** field of the request.

The returned **Status** is copied into the **SMB_Header.Status** field of the response. If the operation is successful, then the following additional mapping of output elements applies:

- **OutputBuffer** is copied into the **NT_Trans_Data** field of the response.
- **ByteCount** is copied into the **SMB_Parameters.TotalDataCount** field of the response.

If quotas are disabled then the object store returns the **ChangeTime**, **QuotaUsed**, **QuotaThreshold**, and **QuotaLimit** fields set to zero in the FILE_QUOTA_INFORMATION.

Windows-based servers enumerate and return quota information for all SIDs on the file instead of the SIDs specified in the **SidList** field, if any of the following conditions are TRUE:

- **SidListLength** is zero.
- **StartSidOffset** is less than **SidListLength**.
- **StartSidOffset** or **SidListLength** is greater than **SMB_Parameters.Words.DataCount**.

[&lt;137&gt; Section 3.3.5.11.3](#Appendix_A_Target_137): Windows-based servers set the quota information on the object store, as specified in \[MS-FSA\] section 2.1.5.14.10, if **Server.Open** is on a file. If **Server.Open** is on a directory, then processing follows, as specified in \[MS-FSA\] section 2.1.5.21, with the following mapping of input elements:

- **Open** is an Open of **Server.Open.TreeConnect.Share.LocalPath** for the **Server.Open** indicated by the **NT_Trans_Parameters.FID** field of the request.
- **InputBuffer** is the **NT_Trans_Data.QuotaInformation** field of the request.
- **InputBuffer** is set to the size, in bytes, of the **InputBuffer** field.

[&lt;138&gt; Section 3.3.5.11.4](#Appendix_A_Target_138): Windows 2000, Windows Server 2003, Windows Server 2003 R2, Windows XP, Windows Vista, and Windows Server 2008 servers fail the request but respond with arbitrary values in the NT_TRANSACT_CREATE Response. Windows 7, Windows Server 2008 R2, Windows 8, Windows Server 2012, Windows 8.1, Windows Server 2012 R2, Windows 10, and Windows Server 2016 send an error response message without the parameter block or the data block.

# Change Tracking

This section identifies changes that were made to this document since the last release. Changes are classified as New, Major, Minor, Editorial, or No change.

The revision class **New** means that a new document is being released.

The revision class **Major** means that the technical content in the document was significantly revised. Major changes affect protocol interoperability or implementation. Examples of major changes are:

- A document revision that incorporates changes to interoperability requirements or functionality.
- The removal of a document from the documentation set.

The revision class **Minor** means that the meaning of the technical content was clarified. Minor changes do not affect protocol interoperability or implementation. Examples of minor changes are updates to clarify ambiguity at the sentence, paragraph, or table level.

The revision class **Editorial** means that the formatting in the technical content was changed. Editorial changes apply to grammatical, formatting, and style issues.

The revision class **No change** means that no new technical changes were introduced. Minor editorial and formatting changes may have been made, but the technical content of the document is identical to the last released version.

Major and minor changes can be described further using the following change types:

- New content added.
- Content updated.
- Content removed.
- New product behavior note added.
- Product behavior note updated.
- Product behavior note removed.
- New protocol syntax added.
- Protocol syntax updated.
- Protocol syntax removed.
- New content added due to protocol revision.
- Content updated due to protocol revision.
- Content removed due to protocol revision.
- New protocol syntax added due to protocol revision.
- Protocol syntax updated due to protocol revision.
- Protocol syntax removed due to protocol revision.
- Obsolete document removed.

Editorial changes are always classified with the change type **Editorially updated**.

Some important terms used in the change type descriptions are defined as follows:

- **Protocol syntax** refers to data elements (such as packets, structures, enumerations, and methods) as well as interfaces.
- **Protocol revision** refers to changes made to a protocol that affect the bits that are sent over the wire.

The changes made to this document are listed in the following table. For more information, please contact [dochelp@microsoft.com](mailto:dochelp@microsoft.com).

| Section                                                                           | Tracking number (if applicable) and description                   | Major change (Y or N) | Change type                    |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------- | --------------------- | ------------------------------ |
| [2.2.4.6.2](#Section_e5a467bccd364afa825e3f2a7bfd6189) Server Response Extensions | Removed behavior notes from the NativeOS and NativeLanMan fields. | Y                     | Product behavior note removed. |

# Index

\_

[\_\_packet\_\_ packet](#section_3527e3a938de444eaed28d300fbf0c09) 73

3

[32-bit status codes](#section_6ab6ca20b40441fdb91a2ed39e3762ea) 31

A

Abstract data model

client ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.2.1](#section_20e8c664e3ff4d4a95f98f08efcfcac3) 98)

server ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.3.1](#section_abf6ea42519e4245babb5b717919484e) 117)

Algorithms

[Copychunk Resume Key generation](#section_14789215fc374f4a81901972cc0fd436) 26

[field generation](#section_44c3cf8d093149238fdc738537ba70ba) 26

[VolumeId generation](#section_8fd048dcf28a40e896e387c6151706b3) 26

[Applicability](#section_4e465d5f123841edb537776b5f08b258) 18

C

[Capability negotiation](#section_57c3e55af0e24b8e9a857837a3738abb) 19

[Change tracking](#section_4caa15a3700f479f90ce53e2ecca945d) 177

Client

abstract data model ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.2.1](#section_20e8c664e3ff4d4a95f98f08efcfcac3) 98)

higher-layer triggered events ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.2.4](#section_7ae40ac0eae64c598793350fe14caadd) 100)

initialization ([section 3.1.3](#section_7eb896348b214a798f490de6ed31d8ee) 97, [section 3.2.3](#section_23f5bb29222c42089352c2f9b4a86382) 100)

local events ([section 3.1.7](#section_fd49fdab42f84a4e882e519d0fc95fe8) 98, [section 3.2.7](#section_d62a1b92411c4948a04f77e81a5d2d6c) 117)

message processing ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.2.5](#section_dff83b6038e04072b8f58801d1e75c82) 112)

[other local events](#section_d62a1b92411c4948a04f77e81a5d2d6c) 117

sequencing rules ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.2.5](#section_dff83b6038e04072b8f58801d1e75c82) 112)

timer events ([section 3.1.6](#section_65632e87fe1b43409bdda398d91bc647) 98, [section 3.2.6](#section_175a98809cfa4166889b4c324e3111f0) 117)

timers ([section 3.1.2](#section_7c27252f3548456db434e867fa8baa28) 97, [section 3.2.2](#section_2b70d101bcdf4cb5ab48bff41993d11a) 99)

Client details ([section 3.1](#section_52cda59356a349a19c9a97ad243c4d4a) 97, [section 3.2](#section_05c920ddb64340e6872f95b4ce1a9104) 98)

[Copychunk Resume Key generation algorithm](#section_14789215fc374f4a81901972cc0fd436) 26

D

Data model - abstract

client ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.2.1](#section_20e8c664e3ff4d4a95f98f08efcfcac3) 98)

server ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.3.1](#section_abf6ea42519e4245babb5b717919484e) 117)

[Direct_TCP_Transport packet](#section_f906c680330c43ae9a71f854e24aeee6) 21

[Directory_Access_Mask packet](#section_d524144c3cfc49c3903c284e5adbd60a) 28

E

[Examples](#section_2e8fe498ea8c46bdb9fef3cc6ce25159) 134

[Extended attribute encoding extensions](#section_65e0c225592544b081046b91339c709f) 23

F

[Field generation algorithm](#section_44c3cf8d093149238fdc738537ba70ba) 26

[Fields - vendor-extensible](#section_f99c712ef9984bbc9125cfe2010b6e44) 19

[File system attribute extensions](#section_3065351b0b7849769a5a11657d8857c7) 24

[File_Pipe_Printer_Access_Mask packet](#section_27f99d2977844684b6dd264e9025b286) 26

[FSCTL_SRV_COPYCHUNK Response packet](#section_94a4d71a5e414ddf893cea6ab5388ba3) 81

[FSCTL_SRV_ENUMERATE_SNAPSHOTS Response packet](#section_5a43eb2950c846b68319e793a11f6226) 79

[FSCTL_SRV_REQUEST_RESUME_KEY Response packet](#section_c2571af45f264bfcba6738d26f16effc) 80

G

[Glossary](#section_c7d64f171ab64151b9e8f15813235c83) 9

H

Higher-layer triggered events

client ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.2.4](#section_7ae40ac0eae64c598793350fe14caadd) 100)

server ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.3.4](#section_90ca3cd794d74091987ba099a551602c) 119)

I

[Implementer - security considerations](#section_928b1e9424084606846ff7c6111d4ca5) 157

[Index of security parameters](#section_0578af7bf36e448d86640e5c5ef62391) 157

[Information Levels message](#section_e40abab428d148278c224409d33605b8) 89

[Informative references](#section_f8178eacd7d6476e9d97cf61125668a0) 15

Initialization

client ([section 3.1.3](#section_7eb896348b214a798f490de6ed31d8ee) 97, [section 3.2.3](#section_23f5bb29222c42089352c2f9b4a86382) 100)

server ([section 3.1.3](#section_7eb896348b214a798f490de6ed31d8ee) 97, [section 3.3.3](#section_6d46923dce8c40e190c1d702bf53d0e3) 119)

[Introduction](#section_fd2a8346941440e281b1ed294f9768ea) 9

L

Local events

client ([section 3.1.7](#section_fd49fdab42f84a4e882e519d0fc95fe8) 98, [section 3.2.7](#section_d62a1b92411c4948a04f77e81a5d2d6c) 117)

server ([section 3.1.7](#section_fd49fdab42f84a4e882e519d0fc95fe8) 98, [section 3.3.7](#section_607b3c6417d84f488f92a4d1be96b266) 133)

M

Message processing

client ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.2.5](#section_dff83b6038e04072b8f58801d1e75c82) 112)

server ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.3.5](#section_5551a31c17aa43629f2b2d8b2d4bedd6) 121)

Messages

[Information Levels](#section_e40abab428d148278c224409d33605b8) 89

[syntax](#section_6cdbc7263e4a499982b3f5e3d7f3c37a) 21

[transport](#section_f906c680330c43ae9a71f854e24aeee6) 21

N

[Normative references](#section_2d91bdcaf941428a9f89bcabebab4dce) 14

[NT_Trans_Parameters Client_Request_Extension packet](#section_25c118f24c5d4afab06c64b71368076c) 71

[NT_TRANSACT_CREATE2](#section_75a3a815d2c64c948d668221869c7975) 30

[NT_TRANSACT_IOCTL](#section_2f8a9baed8c1462893daadba011e4f17) 76

[NT_TRANSACT_IOCTL_Client_Request_Extension packet](#section_2f8a9baed8c1462893daadba011e4f17) 76

[NT_TRANSACT_QUERY_QUOTA](#section_75a3a815d2c64c948d668221869c7975) 30

[NT_TRANSACT_QUERY_QUOTA_Client_Request packet](#section_9f3f73f99c4a42ba9f56e6352491d714) 84

[NT_TRANSACT_QUERY_QUOTA_Server_Response packet](#section_178d375d029c40aeb7b8bd01547fff51) 85

[NT_TRANSACT_SET_QUOTA](#section_75a3a815d2c64c948d668221869c7975) 30

[NT_TRANSACT_SET_QUOTA_Client_Request packet](#section_5172dc9ce7ad47fa86c0317b047a37eb) 87

O

Other local events

[client](#section_d62a1b92411c4948a04f77e81a5d2d6c) 117

[server](#section_607b3c6417d84f488f92a4d1be96b266) 133

[Overview (synopsis)](#section_7c30a2a09c9a423b99827a49979af7d4) 16

P

[Parameters - security index](#section_0578af7bf36e448d86640e5c5ef62391) 157

[Preconditions](#section_493935193f254ae9ac28a7b491d6239b) 18

[Prerequisites](#section_493935193f254ae9ac28a7b491d6239b) 18

[Product behavior](#section_ecd51ae2478c455b8669254b74208d3b) 158

Protocol Details

[overview](#section_1f7aaa2b0b3f4eca908b8a6417dca534) 97

R

[References](#section_62c700dc724d4302b7d270cd7ee86ebc) 14

[informative](#section_f8178eacd7d6476e9d97cf61125668a0) 15

[normative](#section_2d91bdcaf941428a9f89bcabebab4dce) 14

[Relationship to other protocols](#section_592d0cbe41d04b8e8bb9a60edd85e5e8) 16

S

Security

[implementer considerations](#section_928b1e9424084606846ff7c6111d4ca5) 157

[parameter index](#section_0578af7bf36e448d86640e5c5ef62391) 157

Sequencing rules

client ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.2.5](#section_dff83b6038e04072b8f58801d1e75c82) 112)

server ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.3.5](#section_5551a31c17aa43629f2b2d8b2d4bedd6) 121)

Server

abstract data model ([section 3.1.1](#section_1fa7fd17def14bd7b1dd41c5a8ce7362) 97, [section 3.3.1](#section_abf6ea42519e4245babb5b717919484e) 117)

higher-layer triggered events ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.3.4](#section_90ca3cd794d74091987ba099a551602c) 119)

initialization ([section 3.1.3](#section_7eb896348b214a798f490de6ed31d8ee) 97, [section 3.3.3](#section_6d46923dce8c40e190c1d702bf53d0e3) 119)

local events ([section 3.1.7](#section_fd49fdab42f84a4e882e519d0fc95fe8) 98, [section 3.3.7](#section_607b3c6417d84f488f92a4d1be96b266) 133)

message processing ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.3.5](#section_5551a31c17aa43629f2b2d8b2d4bedd6) 121)

[other local events](#section_607b3c6417d84f488f92a4d1be96b266) 133

sequencing rules ([section 3.1.5](#section_a07570c4cf07416597c8205fbf1b0fb0) 98, [section 3.3.5](#section_5551a31c17aa43629f2b2d8b2d4bedd6) 121)

timer events ([section 3.1.6](#section_65632e87fe1b43409bdda398d91bc647) 98, [section 3.3.6](#section_13c8b89c0cd24f338b52255383cd4038) 133)

timers ([section 3.1.2](#section_7c27252f3548456db434e867fa8baa28) 97, [section 3.3.2](#section_a5bf22fb240743d2b975d02512e51a78) 118)

Server details ([section 3.1](#section_52cda59356a349a19c9a97ad243c4d4a) 97, [section 3.3](#section_0dd233263fdb41a1b950170b52a0879e) 117)

[SMB_COM_NEGOTIATE_Extended_Security_Response packet](#section_d883d0a55a0a46268e3e87b0b66b79aa) 44

[SMB_COM_NEGOTIATE_Non_Extended_Security_Response packet](#section_a8f7396e625147b29d318a83cc230d21) 49

[SMB_COM_NT_CREATE_ANDX_Client_Request_Extensions packet](#section_8e14ed93f27544d1bc46dfaf296c91b1) 60

[SMB_COM_NT_CREATE_ANDX_Server_Response_Extension packet](#section_9e7d187492bd44098089ffc1a4a4d94e) 62

[SMB_COM_NT_TRANSACTION](#section_602802fd5870433a955c79897847053e) 60

[SMB_COM_OPEN_ANDX_Client_Request_Extensions packet](#section_5b2e082db4724569a2014efc0b8e0f5f) 36

[SMB_COM_OPEN_ANDX_Server_Response_Extensions packet](#section_2e946bbf5e0f4521b68398a7a4801c3c) 37

[SMB_COM_READ_ANDX_Client_Request_Extensions packet](#section_df9244e87b2d4714a3836990589a8ff4) 39

[SMB_COM_READ_ANDX_Server_Response_Extensions packet](#section_54dd2a6b299c4c9b9f8871c5b0511f6e) 41

[SMB_COM_SESSION_SETUP_ANDX_Client_Request_Extensions packet](#section_a00d03613544484596ab309b4bb7705d) 52

[SMB_COM_SESSION_SETUP_ANDX_Server_Response_Extensions packet](#section_e5a467bccd364afa825e3f2a7bfd6189) 54

[SMB_COM_TRANSACTION2](#section_714bb6fa7fab4dab8ff88a01c273b9ce) 44

[SMB_COM_TREE_CONNECT_ANDX_Client_Request_Extensions packet](#section_16b173568eff49c29d21557e07ef085d) 57

[SMB_COM_TREE_CONNECT_ANDX_Server_Response_Extensions packet](#section_087860d5391941d5a7531b330d651196) 58

[SMB_COM_WRITE_ANDX_Client_Request_Extensions packet](#section_178be656705649ea8bcbcf123737b016) 42

[SMB_COM_WRITE_ANDX_Server_Response_Extensions packet](#section_056d7d3304574f9ab7e0ab983ce24ae4) 43

[SMB_FIND_FILE_BOTH_DIRECTORY_INFO_Extensions packet](#section_03d05a6fbbaf4a9ea556036581b02737) 90

[SMB_FIND_FILE_ID_BOTH_DIRECTORY_INFO packet](#section_fbae2fc37ff24437b0b09536a73bb6ea) 93

[SMB_FIND_FILE_ID_FULL_DIRECTORY_INFO packet](#section_714e4211dd39453caf0b846cbf661489) 92

[SMB_Header_Extensions_and packet](#section_3c0848a6efe947c2b57af7e8217150b9) 34

[SRV_COPYCHUNK packet](#section_e4e201827c714755b638bb75ff1c06ca) 78

[Standards assignments](#section_acc6e3bd3aba42beb15f99397399ab51) 19

[StatusCodes](#section_6ab6ca20b40441fdb91a2ed39e3762ea) 31

[Syntax - message](#section_6cdbc7263e4a499982b3f5e3d7f3c37a) 21

T

Timer events

client ([section 3.1.6](#section_65632e87fe1b43409bdda398d91bc647) 98, [section 3.2.6](#section_175a98809cfa4166889b4c324e3111f0) 117)

server ([section 3.1.6](#section_65632e87fe1b43409bdda398d91bc647) 98, [section 3.3.6](#section_13c8b89c0cd24f338b52255383cd4038) 133)

Timers

client ([section 3.1.2](#section_7c27252f3548456db434e867fa8baa28) 97, [section 3.2.2](#section_2b70d101bcdf4cb5ab48bff41993d11a) 99)

server ([section 3.1.2](#section_7c27252f3548456db434e867fa8baa28) 97, [section 3.3.2](#section_a5bf22fb240743d2b975d02512e51a78) 118)

[Tracking changes](#section_4caa15a3700f479f90ce53e2ecca945d) 177

[TRANS_CALL_NMPIPE](#section_75a3a815d2c64c948d668221869c7975) 30

[TRANS_RAW_READ_NMPIPE](#section_75a3a815d2c64c948d668221869c7975) 30

[TRANS2_SET_FS_INFORMATION](#section_75a3a815d2c64c948d668221869c7975) 30

[TRANS2_SET_FS_INFORMATION_Client_Request packet](#section_cf5f012fe1c4499d9df8a95add99221d) 67

[Transport](#section_f906c680330c43ae9a71f854e24aeee6) 21

[Transport - message](#section_f906c680330c43ae9a71f854e24aeee6) 21

Triggered events - higher-layer

client ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.2.4](#section_7ae40ac0eae64c598793350fe14caadd) 100)

server ([section 3.1.4](#section_cdfc37e306b744f0aeba387eeef85592) 97, [section 3.3.4](#section_90ca3cd794d74091987ba099a551602c) 119)

V

[Vendor-extensible fields](#section_f99c712ef9984bbc9125cfe2010b6e44) 19

[Versioning](#section_57c3e55af0e24b8e9a857837a3738abb) 19

[VolumeId generation algorithm](#section_8fd048dcf28a40e896e387c6151706b3) 26