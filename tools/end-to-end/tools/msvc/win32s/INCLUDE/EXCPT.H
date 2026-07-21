/***
*excpt.h - defines exception values, types and routines
*
*	Copyright (c) 1990-1993, Microsoft Corporation. All rights reserved.
*
*Purpose:
*	This file contains the definitions and prototypes for the compiler-
*	dependent intrinsics, support functions and keywords which implement
*	the structured exception handling extensions.
*
****/

#ifndef _INC_EXCPT

#ifdef __cplusplus
extern "C" {
#endif



/*
 * Exception disposition return values.
 */
typedef enum _EXCEPTION_DISPOSITION {
    ExceptionContinueExecution,
    ExceptionContinueSearch,
    ExceptionNestedException,
    ExceptionCollidedUnwind
} EXCEPTION_DISPOSITION;


/*
 * Prototype for SEH support function.
 */

#ifdef	_M_IX86

/*
 * Declarations to keep MS C 8 (386/486) compiler happy
 */
struct _EXCEPTION_RECORD;
struct _CONTEXT;

EXCEPTION_DISPOSITION __cdecl _except_handler (
	struct _EXCEPTION_RECORD *ExceptionRecord,
	void * EstablisherFrame,
	struct _CONTEXT *ContextRecord,
	void * DispatcherContext
	);

#endif


/*
 * Keywords and intrinsics for SEH
 */

#if   ( (_MSC_VER >= 800) && (_M_IX86 >= 300) )
/*
 * MS C8-32 (386/486)
 */
#define GetExceptionCode		_exception_code
#define exception_code			_exception_code
#define GetExceptionInformation 	(struct _EXCEPTION_POINTERS *)_exception_info
#define exception_info			(struct _EXCEPTION_POINTERS *)_exception_info
#define AbnormalTermination		_abnormal_termination
#define abnormal_termination		_abnormal_termination

unsigned long __cdecl _exception_code(void);
void *	      __cdecl _exception_info(void);
int	      __cdecl _abnormal_termination(void);

#endif


/*
 * Legal values for expression in except().
 */

#define EXCEPTION_EXECUTE_HANDLER	 1
#define EXCEPTION_CONTINUE_SEARCH	 0
#define EXCEPTION_CONTINUE_EXECUTION	-1



#ifdef __cplusplus
}
#endif

#define _INC_EXCPT
#endif	/* _INC_EXCPT */
