(defpackage #:document-reader
  (:use #:cl)
  (:export #:enable-document-reader
           #:disable-document-reader
           #:*document-parts*))

(in-package #:document-reader)

(defvar *original-readtable* (copy-readtable)
  "A copy of the standard readtable to allow us to switch back.")

(defvar *document-readtable* (copy-readtable)
  "Our custom readtable for parsing mixed files.")

(defvar *document-parts* nil
  "A dynamically-bound list to collect the processed document parts.")

;;; Reader Macro Functions

(defun read-heading (stream char)
  "Reader for lines starting with '#'. Produces a :heading form."
  (declare (ignore char))
  (let ((level 1))
    (loop for next-char = (peek-char nil stream t nil t)
          while (char= next-char #\#)
          do (progn (read-char stream t nil t) (incf level)))
    ;; consume space
    (when (char= (peek-char nil stream t nil t) #\ )
      (read-char stream t nil t))
    `(:heading ,level ,(read-line stream))))

(defun read-spliced-text (stream char)
  "Reader for lines starting with plain text.
  Produces a (concatenate 'string ...) form."
  (unread-char char stream) ; Put the character that triggered this back.
  (let ((parts nil))
    (loop for next-char = (peek-char nil stream t nil t)
          until (or (null next-char) (char= next-char #\newline))
          do (if (char= next-char #\()
                 (push (read stream t nil t) parts)
                 (push (read-text-until-paren-or-newline stream) parts)))
    ;; If the line was just whitespace, read returns nil.
    (when parts
      `(concatenate 'string ,@(mapcar (lambda (part)
                                        (if (stringp part)
                                            part
                                            `(princ-to-string ,part)))
                                      (nreverse parts))))))

(defun read-text-until-paren-or-newline (stream)
  "Helper to read plain text into a string."
  (with-output-to-string (s)
    (loop for char = (peek-char nil stream t nil t)
          while (and char (not (char= char #\()) (not (char= char #\newline)))
          do (write-char (read-char stream) s))))

;;; Set up the Custom Readtable

(defun setup-document-readtable ()
  ;; '#' is now our heading macro
  (set-macro-character #\# #'read-heading nil *document-readtable*)

  ;; Any letter will trigger our text-splicing reader
  (loop for char-code from (char-code #\a) to (char-code #\z)
        do (set-macro-character (code-char char-code) #'read-spliced-text nil *document-readtable*))
  (loop for char-code from (char-code #\A) to (char-code #\Z)
        do (set-macro-character (code-char char-code) #'read-spliced-text nil *document-readtable*)))

(setup-document-readtable)

;;; Public Interface

(defun enable-document-reader ()
  "Switches the Lisp reader to our custom document-parsing mode."
  (setf *readtable* *document-readtable*))

(defun disable-document-reader ()
  "Switches the Lisp reader back to the standard mode."
  (setf *readtable* *original-readtable*))
