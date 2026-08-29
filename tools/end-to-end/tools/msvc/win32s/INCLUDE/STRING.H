/***
*string.h - declarations for string manipulation functions
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file contains the function declarations for the string
*	manipulation functions.
*	[ANSI/System V]
*
****/

#ifndef _INC_STRING

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


/* function prototypes */

_CRTIMP void * __cdecl _memccpy(void *, const void *, int, unsigned int);
_CRTIMP void * __cdecl memchr(const void *, int, size_t);
	int __cdecl memcmp(const void *, const void *, size_t);
_CRTIMP int __cdecl _memicmp(const void *, const void *, unsigned int);
	void * __cdecl memcpy(void *, const void *, size_t);
_CRTIMP void * __cdecl memmove(void *, const void *, size_t);
	void * __cdecl memset(void *, int, size_t);
	char * __cdecl strcat(char *, const char *);
_CRTIMP char * __cdecl strchr(const char *, int);
	int __cdecl strcmp(const char *, const char *);
_CRTIMP int __cdecl _strcmpi(const char *, const char *);
_CRTIMP int __cdecl _stricmp(const char *, const char *);
_CRTIMP int __cdecl strcoll(const char *, const char *);
_CRTIMP int __cdecl _stricoll(const char *, const char *);
	char * __cdecl strcpy(char *, const char *);
_CRTIMP size_t __cdecl strcspn(const char *, const char *);
_CRTIMP char * __cdecl _strdup(const char *);
_CRTIMP char * __cdecl _strerror(const char *);
_CRTIMP char * __cdecl strerror(int);
	size_t __cdecl strlen(const char *);
_CRTIMP char * __cdecl _strlwr(char *);
_CRTIMP char * __cdecl strncat(char *, const char *, size_t);
_CRTIMP int __cdecl strncmp(const char *, const char *, size_t);
_CRTIMP int __cdecl _strnicmp(const char *, const char *, size_t);
_CRTIMP char * __cdecl strncpy(char *, const char *, size_t);
_CRTIMP char * __cdecl _strnset(char *, int, size_t);
_CRTIMP char * __cdecl strpbrk(const char *, const char *);
_CRTIMP char * __cdecl strrchr(const char *, int);
_CRTIMP char * __cdecl _strrev(char *);
	char * __cdecl _strset(char *, int);
_CRTIMP size_t __cdecl strspn(const char *, const char *);
_CRTIMP char * __cdecl strstr(const char *, const char *);
_CRTIMP char * __cdecl strtok(char *, const char *);
_CRTIMP char * __cdecl _strupr(char *);
_CRTIMP size_t __cdecl strxfrm (char *, const char *, size_t);

#ifndef _WSTRING_DEFINED
_CRTIMP wchar_t * __cdecl wcscat(wchar_t *, const wchar_t *);
_CRTIMP wchar_t * __cdecl wcschr(const wchar_t *, wchar_t);
_CRTIMP int __cdecl wcscmp(const wchar_t *, const wchar_t *);
_CRTIMP wchar_t * __cdecl wcscpy(wchar_t *, const wchar_t *);
_CRTIMP size_t __cdecl wcscspn(const wchar_t *, const wchar_t *);
_CRTIMP size_t __cdecl wcslen(const wchar_t *);
_CRTIMP wchar_t * __cdecl wcsncat(wchar_t *, const wchar_t *, size_t);
_CRTIMP int __cdecl wcsncmp(const wchar_t *, const wchar_t *, size_t);
_CRTIMP wchar_t * __cdecl wcsncpy(wchar_t *, const wchar_t *, size_t);
_CRTIMP wchar_t * __cdecl wcspbrk(const wchar_t *, const wchar_t *);
_CRTIMP wchar_t * __cdecl wcsrchr(const wchar_t *, wchar_t);
_CRTIMP size_t __cdecl wcsspn(const wchar_t *, const wchar_t *);
_CRTIMP wchar_t * __cdecl wcsstr(const wchar_t *, const wchar_t *);
_CRTIMP wchar_t * __cdecl wcstok(wchar_t *, const wchar_t *);

_CRTIMP wchar_t * __cdecl _wcsdup(const wchar_t *);
_CRTIMP int __cdecl _wcsicmp(const wchar_t *, const wchar_t *);
_CRTIMP int __cdecl _wcsnicmp(const wchar_t *, const wchar_t *, size_t);
_CRTIMP wchar_t * __cdecl _wcsnset(wchar_t *, wchar_t, size_t);
_CRTIMP wchar_t * __cdecl _wcsrev(wchar_t *);
_CRTIMP wchar_t * __cdecl _wcsset(wchar_t *, wchar_t);

_CRTIMP wchar_t * __cdecl _wcslwr(wchar_t *);
_CRTIMP wchar_t * __cdecl _wcsupr(wchar_t *);
_CRTIMP size_t __cdecl wcsxfrm(wchar_t *, const wchar_t *, size_t);
_CRTIMP int __cdecl wcscoll(const wchar_t *, const wchar_t *);
_CRTIMP int __cdecl _wcsicoll(const wchar_t *, const wchar_t *);

/* old names */
#define wcswcs wcsstr

#define _WSTRING_DEFINED
#endif

#if !__STDC__
_CRTIMP void * __cdecl memccpy(void *, const void *, int, unsigned int);
_CRTIMP int __cdecl memicmp(const void *, const void *, unsigned int);
_CRTIMP int __cdecl strcmpi(const char *, const char *);
_CRTIMP int __cdecl stricmp(const char *, const char *);
_CRTIMP char * __cdecl strdup(const char *);
_CRTIMP char * __cdecl strlwr(char *);
_CRTIMP int __cdecl strnicmp(const char *, const char *, size_t);
_CRTIMP char * __cdecl strnset(char *, int, size_t);
_CRTIMP char * __cdecl strrev(char *);
	char * __cdecl strset(char *, int);
_CRTIMP char * __cdecl strupr(char *);

#define wcsdup	_wcsdup
#define wcsicmp	_wcsicmp
#define wcsnicmp _wcsnicmp
#define wcsnset	_wcsnset
#define wcsrev	_wcsrev
#define wcsset	_wcsset
#define wcslwr	_wcslwr
#define wcsupr	_wcsupr
#define wcsicoll _wcsicoll
#endif	/* !__STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_STRING
#endif	/* _INC_STRING */
