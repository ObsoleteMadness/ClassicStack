/***
*sys/stat.h - defines structure used by stat() and fstat()
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file defines the structure used by the _stat() and _fstat()
*	routines.
*	[System V]
*
****/

#ifndef _INC_STAT

#ifdef __cplusplus
extern "C" {
#endif


#ifndef _CRTIMP
#ifdef _DLL
#define _CRTIMP __declspec(dllimport)
#else
#define _CRTIMP
#endif
#endif


#include <sys/types.h>


#ifndef _TIME_T_DEFINED
typedef long time_t;
#define _TIME_T_DEFINED
#endif

/* define structure for returning status information */

#ifndef _STAT_DEFINED

#ifdef	_M_IX86
#pragma pack(push,4)
#endif	/* _M_IX86 */

struct _stat {
	_dev_t st_dev;
	_ino_t st_ino;
	unsigned short st_mode;
	short st_nlink;
	short st_uid;
	short st_gid;
	_dev_t st_rdev;
	_off_t st_size;
	time_t st_atime;
	time_t st_mtime;
	time_t st_ctime;
	};

#if !__STDC__

/* Non-ANSI names for compatibility */

struct stat {
	_dev_t st_dev;
	_ino_t st_ino;
	unsigned short st_mode;
	short st_nlink;
	short st_uid;
	short st_gid;
	_dev_t st_rdev;
	_off_t st_size;
	time_t st_atime;
	time_t st_mtime;
	time_t st_ctime;
	};

#endif	/* __STDC__ */

#ifdef	_M_IX86
#pragma pack(pop)
#endif	/* _M_IX86 */

#define _STAT_DEFINED
#endif

#define _S_IFMT 	0170000 	/* file type mask */
#define _S_IFDIR	0040000 	/* directory */
#define _S_IFCHR	0020000 	/* character special */
#define _S_IFIFO	0010000 	/* pipe */
#define _S_IFREG	0100000 	/* regular */
#define _S_IREAD	0000400 	/* read permission, owner */
#define _S_IWRITE	0000200 	/* write permission, owner */
#define _S_IEXEC	0000100 	/* execute/search permission, owner */


/* function prototypes */

_CRTIMP int __cdecl _fstat(int, struct _stat *);
_CRTIMP int __cdecl _stat(const char *, struct _stat *);

#if !__STDC__

/* Non-ANSI names for compatibility */

#define S_IFMT	 _S_IFMT
#define S_IFDIR  _S_IFDIR
#define S_IFCHR  _S_IFCHR
#define S_IFREG  _S_IFREG
#define S_IREAD  _S_IREAD
#define S_IWRITE _S_IWRITE
#define S_IEXEC  _S_IEXEC

_CRTIMP int __cdecl fstat(int, struct stat *);
_CRTIMP int __cdecl stat(const char *, struct stat *);

#endif	/* __STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_STAT
#endif	/* _INC_STAT */
