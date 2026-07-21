/***
*stdlib.h - declarations/definitions for commonly used library functions
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This include file contains the function declarations for
*	commonly used library functions which either don't fit somewhere
*	else, or, like toupper/tolower, can't be declared in the normal
*	place for other reasons.
*	[ANSI]
*
****/

#ifndef _INC_STDLIB

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


#ifndef _WCHAR_T_DEFINED
typedef unsigned short wchar_t;
#define _WCHAR_T_DEFINED
#endif


/* define NULL pointer value */

#ifndef NULL
#ifdef __cplusplus
#define NULL	0
#else
#define NULL	((void *)0)
#endif
#endif


/* definition of the return type for the onexit() function */

#define EXIT_SUCCESS	0
#define EXIT_FAILURE	1


#ifndef _ONEXIT_T_DEFINED
typedef int (__cdecl * _onexit_t)(void);
#if !__STDC__
/* Non-ANSI name for compatibility */
#define onexit_t _onexit_t
#endif
#define _ONEXIT_T_DEFINED
#endif


/* Data structure definitions for div and ldiv runtimes. */

#ifndef _DIV_T_DEFINED

typedef struct _div_t {
	int quot;
	int rem;
} div_t;

typedef struct _ldiv_t {
	long quot;
	long rem;
} ldiv_t;

#define _DIV_T_DEFINED
#endif

/* Maximum value that can be returned by the rand function. */

#define RAND_MAX 0x7fff

/* max mb-len for current locale */

#ifndef MB_CUR_MAX
/* also defined in ctype.h */
#define MB_CUR_MAX __mb_cur_max
_CRTIMP extern int __mb_cur_max;
#endif /* MB_CUR_MAX */

/* min and max macros */

#define __max(a,b)	(((a) > (b)) ? (a) : (b))
#define __min(a,b)	(((a) < (b)) ? (a) : (b))


/* sizes for buffers used by the _makepath() and _splitpath() functions.
 * note that the sizes include space for 0-terminator
 */

#define _MAX_PATH	260	/* max. length of full pathname */
#define _MAX_DRIVE	3	/* max. length of drive component */
#define _MAX_DIR	256	/* max. length of path component */
#define _MAX_FNAME	256	/* max. length of file name component */
#define _MAX_EXT	256	/* max. length of extension component */

/* external variable declarations */

#ifdef	_MT
_CRTIMP int * __cdecl _errno(void);
_CRTIMP unsigned long * __cdecl __doserrno(void);
#define errno	    (*_errno())
#define _doserrno   (*__doserrno())
#else	/* ndef _MT */
_CRTIMP extern int errno;			/* XENIX style error number */
_CRTIMP extern unsigned long _doserrno; 	/* OS system error value */
#endif	/* _MT */

_CRTIMP extern char * _sys_errlist[];		/* perror error message table */
_CRTIMP extern int _sys_nerr;			/* # of entries in sys_errlist table */

_CRTIMP extern int __argc;		/* count of cmd line args */
_CRTIMP extern char ** __argv;		/* pointer to table of cmd line args */

_CRTIMP extern char ** _environ;	/* pointer to environment table */

_CRTIMP extern int _fmode;		/* default file translation mode */
_CRTIMP extern int _fileinfo;		/* open file info mode (for spawn) */

_CRTIMP extern char * _pgmptr;		/* points to the module (EXE) name */

/* Windows major/minor and O.S. version numbers */

_CRTIMP extern unsigned int _osver;
_CRTIMP extern unsigned int _winver;
_CRTIMP extern unsigned int _winmajor;
_CRTIMP extern unsigned int _winminor;

/* function prototypes */

_CRTIMP void   __cdecl abort(void);
	int __cdecl abs(int);
	int    __cdecl atexit(void (__cdecl *)(void));
_CRTIMP double __cdecl atof(const char *);
_CRTIMP int    __cdecl atoi(const char *);
_CRTIMP long   __cdecl atol(const char *);
_CRTIMP void * __cdecl bsearch(const void *, const void *, size_t, size_t,
	int (__cdecl *)(const void *, const void *));
_CRTIMP void * __cdecl calloc(size_t, size_t);
_CRTIMP div_t  __cdecl div(int, int);
_CRTIMP void   __cdecl exit(int);
_CRTIMP void   __cdecl free(void *);
_CRTIMP char * __cdecl getenv(const char *);
_CRTIMP char * __cdecl _itoa(int, char *, int);
	long __cdecl labs(long);
_CRTIMP ldiv_t __cdecl ldiv(long, long);
_CRTIMP char * __cdecl _ltoa(long, char *, int);
_CRTIMP void * __cdecl malloc(size_t);
_CRTIMP int    __cdecl mblen(const char *, size_t);
_CRTIMP size_t __cdecl _mbstrlen(const char *s);
_CRTIMP int    __cdecl mbtowc(wchar_t *, const char *, size_t);
_CRTIMP size_t __cdecl mbstowcs(wchar_t *, const char *, size_t);
_CRTIMP void   __cdecl qsort(void *, size_t, size_t, int (__cdecl *)
	(const void *, const void *));
_CRTIMP int    __cdecl rand(void);
_CRTIMP void * __cdecl realloc(void *, size_t);
_CRTIMP void   __cdecl srand(unsigned int);
_CRTIMP double __cdecl strtod(const char *, char **);
_CRTIMP long   __cdecl strtol(const char *, char **, int);
_CRTIMP unsigned long __cdecl strtoul(const char *, char **, int);
_CRTIMP int    __cdecl system(const char *);
_CRTIMP char * __cdecl _ultoa(unsigned long, char *, int);
_CRTIMP int    __cdecl wctomb(char *, wchar_t);
_CRTIMP size_t __cdecl wcstombs(char *, const wchar_t *, size_t);

#if !__STDC__
#ifndef _WSTDLIB_DEFINED
/* defined in wchar.h officially */
_CRTIMP double __cdecl wcstod(const wchar_t *, wchar_t **);
_CRTIMP long   __cdecl wcstol(const wchar_t *, wchar_t **, int);
_CRTIMP unsigned long __cdecl wcstoul(const wchar_t *, wchar_t **, int);
#define _WSTDLIB_DEFINED
#endif
#endif	/* !__STDC__ */

_CRTIMP char * __cdecl _ecvt(double, int, int *, int *);
_CRTIMP void   __cdecl _exit(int);
_CRTIMP char * __cdecl _fcvt(double, int, int *, int *);
_CRTIMP char * __cdecl _fullpath(char *, const char *, size_t);
_CRTIMP char * __cdecl _gcvt(double, int, char *);
	unsigned long __cdecl _lrotl(unsigned long, int);
	unsigned long __cdecl _lrotr(unsigned long, int);
_CRTIMP void   __cdecl _makepath(char *, const char *, const char *, const char *,
	const char *);
	_onexit_t __cdecl _onexit(_onexit_t);
_CRTIMP void   __cdecl perror(const char *);
_CRTIMP int    __cdecl _putenv(const char *);
	unsigned int __cdecl _rotl(unsigned int, int);
	unsigned int __cdecl _rotr(unsigned int, int);
_CRTIMP void   __cdecl _searchenv(const char *, const char *, char *);
_CRTIMP void   __cdecl _splitpath(const char *, char *, char *, char *, char *);
_CRTIMP void   __cdecl _swab(char *, char *, int);

#ifndef tolower 	/* tolower has been undefined - use function */
_CRTIMP int __cdecl tolower(int);
#endif	/* tolower */

#ifndef toupper 	/* toupper has been undefined - use function */
_CRTIMP int __cdecl toupper(int);
#endif	/* toupper */


#ifndef __STDC__
/* Non-ANSI names for compatibility */

#ifndef __cplusplus
#define max(a,b)	(((a) > (b)) ? (a) : (b))
#define min(a,b)	(((a) < (b)) ? (a) : (b))
#endif

#define sys_errlist _sys_errlist
#define sys_nerr    _sys_nerr
#define environ     _environ

_CRTIMP char * __cdecl ecvt(double, int, int *, int *);
_CRTIMP char * __cdecl fcvt(double, int, int *, int *);
_CRTIMP char * __cdecl gcvt(double, int, char *);
_CRTIMP char * __cdecl itoa(int, char *, int);
_CRTIMP char * __cdecl ltoa(long, char *, int);
	onexit_t __cdecl onexit(onexit_t);
_CRTIMP int    __cdecl putenv(const char *);
_CRTIMP void   __cdecl swab(char *, char *, int);
_CRTIMP char * __cdecl ultoa(unsigned long, char *, int);

#endif	/* __STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_STDLIB
#endif	/* _INC_STDLIB */
