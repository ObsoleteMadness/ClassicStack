/***
*stddef.h - definitions/declarations for common constants, types, variables
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file contains definitions and declarations for some commonly
*	used constants, types, and variables.
*	[ANSI]
*
****/

#ifndef _INC_STDDEF

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


/* define NULL pointer value and the offset() macro */

#ifndef NULL
#ifdef __cplusplus
#define NULL	0
#else
#define NULL	((void *)0)
#endif
#endif


#define offsetof(s,m)	(size_t)&(((s *)0)->m)


/* declare reference to errno */

#ifdef	_MT
_CRTIMP extern int * __cdecl _errno(void);
#define errno	(*_errno())
#else
extern int errno;
#endif


/* define the implementation dependent size types */

#ifndef _PTRDIFF_T_DEFINED
typedef int ptrdiff_t;
#define _PTRDIFF_T_DEFINED
#endif

#ifndef _SIZE_T_DEFINED
typedef unsigned int size_t;
#define _SIZE_T_DEFINED
#endif

#ifndef _WCHAR_T_DEFINED
typedef unsigned short wchar_t;
#define _WCHAR_T_DEFINED
#endif


#ifdef	_MT
_CRTIMP extern unsigned long  __cdecl __threadid(void);
#define _threadid	(__threadid())
_CRTIMP extern unsigned long  __cdecl __threadhandle(void);
#endif

#ifdef __cplusplus
}
#endif

#define _INC_STDDEF
#endif	/* _INC_STDDEF */
