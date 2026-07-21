/***
*direct.h - function declarations for directory handling/creation
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This include file contains the function declarations for the library
*	functions related to directory handling and creation.
*
****/

#ifndef _INC_DIRECT


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

#ifndef _SIZE_T_DEFINED
typedef unsigned int size_t;
#define _SIZE_T_DEFINED
#endif

/* _getdiskfree structure for _getdiskfree() */

#ifndef _DISKFREE_T_DEFINED

struct _diskfree_t {
	unsigned total_clusters;
	unsigned avail_clusters;
	unsigned sectors_per_cluster;
	unsigned bytes_per_sector;
	};

#define _DISKFREE_T_DEFINED
#endif

/* function prototypes */

_CRTIMP int __cdecl _chdir(const char *);
_CRTIMP int __cdecl _chdrive(int);
_CRTIMP char * __cdecl _getcwd(char *, int);
_CRTIMP char * __cdecl _getdcwd(int, char *, int);
_CRTIMP int __cdecl _getdrive(void);
_CRTIMP int __cdecl _mkdir(const char *);
_CRTIMP int __cdecl _rmdir(const char *);
_CRTIMP unsigned __cdecl _getdiskfree(unsigned, struct _diskfree_t *);
_CRTIMP unsigned long __cdecl _getdrives(void);


#if !__STDC__
/* Non-ANSI names for compatibility */
_CRTIMP int __cdecl chdir(const char *);
_CRTIMP char * __cdecl getcwd(char *, int);
_CRTIMP int __cdecl mkdir(const char *);
_CRTIMP int __cdecl rmdir(const char *);
#define diskfree_t  _diskfree_t
#endif	/* __STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_DIRECT
#endif	/* _INC_DIRECT */
