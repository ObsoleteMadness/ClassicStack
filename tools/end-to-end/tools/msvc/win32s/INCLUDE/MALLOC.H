/***
*malloc.h - declarations and definitions for memory allocation functions
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	Contains the function declarations for memory allocation functions;
*	also defines manifest constants and types used by the heap routines.
*	[System V]
*
****/

#ifndef _INC_MALLOC

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

/* maximum heap request that can ever be honored */
#define _HEAP_MAXREQ	0xFFFFFFD8

/* constants for _heapchk/_heapset/_heapwalk routines */
#define _HEAPEMPTY	(-1)
#define _HEAPOK 	(-2)
#define _HEAPBADBEGIN	(-3)
#define _HEAPBADNODE	(-4)
#define _HEAPEND	(-5)
#define _HEAPBADPTR	(-6)
#define _FREEENTRY	0
#define _USEDENTRY	1

#ifndef _HEAPINFO_DEFINED
typedef struct _heapinfo {
	int * _pentry;
	size_t _size;
	int _useflag;
	} _HEAPINFO;
#define _HEAPINFO_DEFINED
#endif

/* external variable declarations */

extern unsigned int _amblksiz;


/* function prototypes */
_CRTIMP void * __cdecl calloc(size_t, size_t);
_CRTIMP void   __cdecl free(void *);
_CRTIMP void * __cdecl malloc(size_t);
_CRTIMP void * __cdecl realloc(void *, size_t);

void * __cdecl _alloca(size_t);
_CRTIMP void * __cdecl _expand(void *, size_t);

_CRTIMP int __cdecl _heapadd(void *, size_t);
_CRTIMP int __cdecl _heapchk(void);
_CRTIMP int __cdecl _heapmin(void);
_CRTIMP int __cdecl _heapset(unsigned int);
_CRTIMP int __cdecl _heapwalk(_HEAPINFO *);
_CRTIMP size_t __cdecl _msize(void *);

#if !__STDC__
/* Non-ANSI names for compatibility */
#define alloca	_alloca
#endif	/* __STDC__*/

#ifdef __cplusplus
}
#endif

#define _INC_MALLOC
#endif	/* _INC_MALLOC */
