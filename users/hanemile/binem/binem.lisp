(defpackage :binem
  (:use #:cl))

(in-package :binem)

(defparameter *command-prefix* "$")
(defparameter *flag-space* (make-hash-table :test 'equal))
(defparameter $main 1337)

(defun $pdf (val)
  "Prints disassembly of a previously analyzed binary"
  (format t "Disasm ~a~%" val))

(defun $e (val)
  "E val uates"
  (format t "~a~%" (eval val)))

(defun $-f (name value)
  "Access to the flag-space"
  (setf (gethash (string-downcase (string name))
                 *flag-space*)
        value)
  (format t "Flag ~a set to ~a~%" name value))

(defun $-fd (name)
  "Delete a flag value"
  (remhash (string-downcase (string name)) *flag-space*)
  (format t "Flag ~a deleted.~%" name))
  
(defun $exit ()
  "Exist the REPL loop"
  (format t "Exiting...~%")
  (throw 'exit nil))

(defun parse-user-input (input)
  (let* ((tokens (read-from-string
                  (concatenate 'string
                               "(" *command-prefix* input ")")))
         (sexp tokens))
    sexp))

(defun start-repl ()
  "Starts the custom REPL loop."
  (catch 'exit
    (loop
      (format t "~a> " *command-prefix*)
      (force-output)
      (handler-case
         (let ((input (read-line)))
           (unless (string= input "")
             (let* ((sexp (parse-user-input input)))
               (if (fboundp (first sexp))
                   (eval sexp)
                   (format t "Unknown command: ~a~%" input)))))
         (error (e)
           (format t "Err: ~a~%" e)
           (force-output))))))

(print (+ 1 2))
(print (apply (first (parse-user-input "pdf")) (list "abc")))
(print (rest (parse-user-input "pdf (+ 2 3)")))

(print (eval (parse-user-input "pdf (+ 2 3)")))
