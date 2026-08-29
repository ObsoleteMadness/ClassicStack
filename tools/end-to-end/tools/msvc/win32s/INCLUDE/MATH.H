/***
*math.h - definitions and declarations for math library
*
*	Copyright (c) 1985-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file contains constant definitions and external subroutine
*	declarations for the math subroutine library.
*	[ANSI/System V]
*
****/

#ifndef _INC_MATH

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


/* definition of _exception struct - this struct is passed to the matherr
 * routine when a floating point exception is detected
 */

#ifndef _EXCEPTION_DEFINED
struct _exception {
	int type;		/* exception type - see below */
	char *name;		/* name of function where error occured */
	double arg1;		/* first argument to function */
	double arg2;		/* second argument (if any) to function */
	double retval;		/* value to be returned by function */
	} ;

#if !__STDC__
/* Non-ANSI name for compatibility */
#define exception _exception
#endif

#define _EXCEPTION_DEFINED
#endif


/* definition of a _complex struct to be used by those who use cabs and
 * want type checking on their argument
 */

#ifndef _COMPLEX_DEFINED
struct _complex {
	double x,y;	/* real and imaginary parts */
	} ;

#if !__STDC__
/* Non-ANSI name for compatibility */
#define complex _complex
#endif

#define _COMPLEX_DEFINED
#endif


/* Constant definitions for the exception type passed in the _exception struct
 */

#define _DOMAIN 	1	/* argument domain error */
#define _SING		2	/* argument singularity */
#define _OVERFLOW	3	/* overflow range error */
#define _UNDERFLOW	4	/* underflow range error */
#define _TLOSS		5	/* total loss of precision */
#define _PLOSS		6	/* partial loss of precision */

#define EDOM		33
#define ERANGE		34


/* definitions of _HUGE and HUGE_VAL - respectively the XENIX and ANSI names
 * for a value returned in case of error by a number of the floating point
 * math routines
 */
_CRTIMP extern double _HUGE;

#define HUGE_VAL _HUGE

/* function prototypes */

	int	__cdecl abs(int);
	double	__cdecl acos(double);
	double	__cdecl asin(double);
	double	__cdecl atan(double);
	double	__cdecl atan2(double, double);
_CRTIMP double	__cdecl atof(const char *);
_CRTIMP double	__cdecl _cabs(struct _complex);
_CRTIMP double	__cdecl ceil(double);
	double	__cdecl cos(double);
	double	__cdecl cosh(double);
	double	__cdecl exp(double);
	double	__cdecl fabs(double);
_CRTIMP double	__cdecl floor(double);
	double	__cdecl fmod(double, double);
_CRTIMP double	__cdecl frexp(double, int *);
_CRTIMP double	__cdecl _hypot(double, double);
_CRTIMP double	__cdecl _j0(double);
_CRTIMP double	__cdecl _j1(double);
_CRTIMP double	__cdecl _jn(int, double);
	long	__cdecl labs(long);
_CRTIMP double	__cdecl ldexp(double, int);
	double	__cdecl log(double);
	double	__cdecl log10(double);
_CRTIMP int	__cdecl _matherr(struct _exception *);
_CRTIMP double	__cdecl modf(double, double *);
	double	__cdecl pow(double, double);
	double	__cdecl sin(double);
	double	__cdecl sinh(double);
	double	__cdecl sqrt(double);
	double	__cdecl tan(double);
	double	__cdecl tanh(double);
_CRTIMP double	__cdecl _y0(double);
_CRTIMP double	__cdecl _y1(double);
_CRTIMP double	__cdecl _yn(int, double);

#define acosl(x)	((long double)acos((double)(x)))
#define asinl(x)	((long double)asin((double)(x)))
#define atanl(x)	((long double)atan((double)(x)))
#define atan2l(x,y)	((long double)atan2((double)(x), (double)(y)))
#define ceill(x)	((long double)ceil((double)(x)))
#define cosl(x)		((long double)cos((double)(x)))
#define coshl(x)	((long double)cosh((double)(x)))
#define expl(x)		((long double)exp((double)(x)))
#define fabsl(x)	((long double)fabs((double)(x)))
#define floorl(x)	((long double)floor((double)(x)))
#define fmodl(x,y)	((long double)fmod((double)(x), (double)(y)))
#define frexpl(x,y)	((long double)frexp((double)(x), (y)))
#define hypotl(x,y)	((long double)hypot((double)(x), (double)(y)))
#define ldexpl(x,y)	((long double)ldexp((double)(x), (y)))
#define logl(x)		((long double)log((double)(x)))
#define log10l(x)	((long double)log10((double)(x)))
#define _matherrl	_matherr
#define modfl(x,y)	((long double)modf((double)(x), (double *)(y)))
#define powl(x,y)	((long double)pow((double)(x), (double)(y)))
#define sinl(x)		((long double)sin((double)(x)))
#define sinhl(x)	((long double)sinh((double)(x)))
#define sqrtl(x)	((long double)sqrt((double)(x)))
#define tanl(x)		((long double)tan((double)(x)))
#define tanhl(x)	((long double)tanh((double)(x)))

#if !__STDC__
/* Non-ANSI names for compatibility */

#define DOMAIN		_DOMAIN
#define SING		_SING
#define OVERFLOW	_OVERFLOW
#define UNDERFLOW	_UNDERFLOW
#define TLOSS		_TLOSS
#define PLOSS		_PLOSS

#define matherr 	_matherr

_CRTIMP extern double HUGE;

_CRTIMP double	__cdecl cabs(struct complex);
_CRTIMP double	__cdecl hypot(double, double);
_CRTIMP double	__cdecl j0(double);
_CRTIMP double	__cdecl j1(double);
_CRTIMP double	__cdecl jn(int, double);
_CRTIMP double	__cdecl y0(double);
_CRTIMP double	__cdecl y1(double);
_CRTIMP double	__cdecl yn(int, double);

#endif	/* __STDC__ */

#ifdef __cplusplus
}
#endif

#define _INC_MATH
#endif	/* _INC_MATH */
