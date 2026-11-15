(defpackage :vokobe2
  (:use :cl)
  (:export #:enable-markdown-syntax
           #:disable-markdown-syntax
           #:process))
(in-package :vokobe2)

;; vokobe2 is the common-lisp version of the original vokobe, which i wrote in 2021
;;
;; it consists of a few stages:
;;
;; STAGE 1: Markdown to Common Lisp
;; STAGE 2: Common Lisp to HTML
;;
;; Things you should be aware of:
;; - the asterisk (*) is redefined, thus let* bindings will break

(defvar *site-index* (make-hash-table :test 'equal))

;; STAGE 1: Markdown -> Common Lisp

;; Header reader macro
;;
;; This works for a single hashtag at the beginning of the line:
;; ... as well as for an infinite amount of hashtags:
;;
;; # title => <h1>title</h1>
;; ###### title => <h6>title</h6>
;;
;; it also works with preceding spaces:
;;
;;    ### title => <h3>title</h3>
;;
;; and if the value before the hashtags can be evaluated
;;
;; 1  ### title => <h3>title</h3>

(defun count-prefix (str prefix)
  "Return the initial portion of the string that matches the prefix
   This can be used to count the amounts of hashtags in a markdown heading:
     (count-prefix \"##### heading\" \#)
     => 5"
  (loop for c across str while (char= c prefix) count c))

(defun gen-heading (level title)
  (format t "<h~a>~a</h~a>" level title level))

(defun make-header-reader (increment)
  "returns a function that reads the line, infers the heading level
   based on the amount of hashtags, adjusts for the amount of hashtags
   present and finally returns the heading"
  (lambda (stream char rest)
    (declare (ignore char) (ignore rest))
    (let* ((cur-line (read-line stream))
           (level (+ increment (count-prefix cur-line #\#)))
           (content (string-trim '(#\Space) (subseq cur-line (- level 1)))))
      (format t "cur-line: ~a level: ~a content: ~a" cur-line level content)
      `(gen-heading ,level ,content))))


;; Bold reader macro
;;
;; Given some text containing *abc*, returns (bold "abc")

(defun bold (content)
  "Returns the HTML representation of the bold object"
  (format nil "<b>~a</b>" content))

(defun read-bold (stream char)
  "read the values in the text marked bold,
   returning the content upon hitting the closing asterisk"
  (declare (ignore char))
  (let ((content '()))
    (loop for ch = (read-char stream nil nil)
          while ch do
            (cond
              ((char= ch #\*)
               (return `(bold ,content)))
              (t
               (setf content (concatenate 'string content (string ch))))))))

;; Reader macros:
;; The previous read-table is stored, so that we can restore it when done
(defvar *previous-readtables* nil)

(defmacro enable-markdown-syntax ()
  '(eval-when (:compile-toplevel :load-toplevel :execute)
     (push *readtable* *previous-readtables*)
     (setq *readtable* (copy-readtable))
     (set-dispatch-macro-character #\# #\Space (make-header-reader 1))
     (set-dispatch-macro-character #\# #\# (make-header-reader 2))))
     ;(set-macro-character #\* #'read-bold)))

(defmacro disable-markdown-syntax ()
  '(eval-when (:compile-toplevel :load-toplevel :execute)
     (setq *readtable* (pop *previous-readtables*))))

(defun process-html-file-b (file-path)
  "Load a file containing HTML expressions and return the results as a list."
  (with-open-file (stream file-path :direction :input :if-does-not-exist nil)
    (read (make-string-input-stream
           (with-output-to-string (*standard-output*)
             ;(enable-markdown-syntax)
             (load stream :verbose t :print nil)
             ;(disable-markdown-syntax))))))

(load "sport.md" :verbose t :print t)

(with-open-file (stream "sport.md" :direction :input)
                                        ;(enable-markdown-syntax)
  (format t "=================~%")
  
  (format t "~a~%"
          (load stream :verbose t :print t)))
  ;(disable-markdown-syntax))))

(process-html-file-b "sport.md")

;;; STAGE 2: Common Lisp -> HTML

(defun line (content)
  "Returns an intermediate representation containing a single line"
  `(line ,content))

(defun par (content)
  "Returns the HTML representation of the paragraph"
  (format nil "<par>~a<par>" content))

(defun find-idx-closing-paren (text start-idx)
  "Given some text and an opening paren, returns the index of the matching closing paren"
  (let ((nest-level 1)
        (end 0))
    (loop for j from (1+ start-idx) below (length text) do
      (let ((current-char (char text j))) 
        (cond
          ((char= current-char #\() (incf nest-level))
          ((char= current-char #\)) (decf nest-level)))
        (when (zerop nest-level)
          (setf end j)
          (return))))
    end))

(defun find-next-char (text char start-index)
  "Find the next occurrence of CHAR in STRING starting from START-INDEX."
  (loop for i from start-index below (length text)
        when (char= (char text i) char)
          do (return i)))

(defun parse-line (line)
  "For all char in a given line, handle lisp expressions and bold statements"
  (let ((tokens '())
        (len (length line)))
    (loop for i from 0 to len do
      (let ((ch (if (< i len)
                    (char line i)
                    nil)))
        (cond
          ; TODO(emile): this can probably be refactored into a nice macro
          
          ; opening parentheses
          ((and ch (char= ch #\())
           (let ((end (find-idx-closing-paren line i)))
             (if end
                 (progn (setf tokens
                              (concatenate 'string
                                           tokens
                                           (format nil
                                                   "~A"
                                                   (eval
                                                    (read-from-string
                                                     (subseq line i (1+ end)))))))
                   (setf i end)))))

          ; opening bold char
          ((and ch (char= ch #\*))
           (let ((end (find-next-char line #\* (1+ i))))
             (if end
                 (progn (setf tokens
                              (concatenate 'string
                                           tokens
                                           (format nil
                                                   "~A"
                                                   (eval
                                                    (read-from-string
                                                     (subseq line i (1+ end)))))))
                   (setf i end)))))
          
          ; otherwise, just append the char to the list of tokens
          (ch (setf tokens (concatenate 'string tokens (string ch)))))))
   (setf tokens (line tokens))
   tokens))

(defun main-loop (stream)
  (let ((metadata '())
        (content '())
        (in-paragraph nil)
        (in-yaml-header t)
        (in-codeblock nil))
       (loop for line = (read-line stream nil nil)
             while line do
              (cond
                ; yaml-metadata
                ((and (typep line 'string)
                      (search ":" line)
                      in-yaml-header)
                 (let ((key (subseq line 0 (search ":" line)))
                       (value (subseq line (+ 2 (search ":" line)) (length line))))
                   (push `(,key . ,value) metadata)))

                ; heading
                ((and (typep line 'string)
                      (search "#" line))
                 (push (eval (read-from-string line))
                       content))

                ; empty line
                ((and (typep line 'string)
                      (equal "" line))
                 (if in-yaml-header (setf in-yaml-header nil))
                 (if in-paragraph (setf in-paragraph nil))
                 (push '(linebreak) content))
                
                ; codeblock
                ((and (typep line 'string)
                      (search "```" line))
                 (if in-codeblock
                     (setf in-codeblock nil)
                     (setf in-codeblock t)))
                ((and (typep line 'string)
                      in-codeblock)
                 (push `(codeline ,line) content))

                ; blockquote
                ((and (typep line 'string)
                      (eq #\> (char line 0)))
                 (push `(blockquote ,(subseq line 2 (length line))) content))

                ; everything else
                (t
                 (if in-paragraph
                     nil
                     (setf in-paragraph t))
                 (let ((parsed-line (parse-line line)))
                   (if parsed-line
                       (push parsed-line
                             content))))))
    `((Metadata . ,metadata) (Content . ,(nreverse content)))))

(defun filename-from-filepath (filepath)
  "returns the content between the last slash and last dot
   abc.md -> abc
   /home/abc/def/test.md -> test"
  (let ((last-slash-index (if (search "/" filepath)
                              (- (length filepath)
                                 (search "/" (reverse filepath)))
                              0))
        (last-dot-index (if (search "." filepath)
                            (- (length filepath)
                               (search "." (reverse filepath)))
                            (length filepath))))
    (string (subseq filepath last-slash-index (- last-dot-index 1)))))

(filename-from-filepath "abc.md")
(filename-from-filepath "/home/abc/def/test.md")

(defun process (filepaths)
  "process the list of markdown files, returning their common lisp interpretation"
  (let ((pages '()))
    (loop for filepath in filepaths do
      (if (probe-file filepath)
          (with-open-file (stream filepath :if-does-not-exist nil)
            (push `(,(filename-from-filepath filepath) . (,(main-loop stream))) pages))
          nil))
    pages))

; how to print all of test.md:
; (format t "test.md all: ~a~%" (assoc "test.md" (process '("sport.md" "test.md"))))

(defun linebreak ()
  (format nil "<br>"))

(defun blockquote (content)
  (format nil "<blockquote>~a</blockquote>" content))

(defun codeline (content)
  (format nil "<pre>~a</pre>" content))

(defun line (content)
  (format nil "<p>~a</p>" content))

(defun merge-lines (type content)
  "Merge consecutive lines of type TYPE into a single one"
  (let ((result '())
        (current-line nil))
    (dolist (item content (reverse result))
      (if (and (consp item) (eq (first item) type))
          (progn
            ;; Merge current line with previous line, if any
            (if current-line
                (setf current-line
                      (append current-line (rest item)))    
                (setf current-line item)))
          (progn
            ;; Save the accumolated line and process the non-LINE item
            (when current-line
              (push current-line result)
              (setf current-line nil))
            (push item result))))
    (when current-line
      (push current-line result))
    result))

(defun lisp2html (pages)
  (format t "----------------~%pages: ~a~%" pages)
  (loop for pagename in pages do
    (let ((content (cdr (assoc 'CONTENT (car (cdr pagename)))))
          (filename (car pagename)))
        (format t "~a.md: ~a~%~a.html: ~a~%" filename content filename (mapcar #'eval content))))
  (format t "html: ~a~%" nil))

(lisp2html (process '("code.md" "test.md")))

(format t "---------~%~a~%"
        (merge-lines 'BLOCKQUOTE
                     (merge-lines 'LINE
                                  (caddr (assoc "test"
                                                (process '("code.md" "test.md"))
                                                :test #'string=)))))


; ---------------------------------

;; Helpers for reader macros
(defun count-prefix (str prefix)
  "Return the initial portion of the string that matches the prefix
   This can be used to count the amounts of hashtags in a markdown heading:
     (count-prefix \"##### heading\" \#)
     => 5"
  (loop for c across str while (char= c prefix) count c))

(eval-when (:compile-toplevel :load-toplevel :execute)
  (defun make-header-reader (increment)
    "returns a function that reads the line, infers the heading level
   based on the amount of hashtags, adjusts for the amount of hashtags
   present and finally returns the heading"
    (lambda (stream char rest)
      (declare (ignore char) (ignore rest))
      (let* ((cur-line (read-line stream))
             (level (+ increment (count-prefix cur-line #\#)))
             (content (string-trim '(#\Space) (subseq cur-line (- level 1)))))
        `(heading ,level ,content)))))

;; (defun read-bold (stream char)
;;   "read the values in the text marked bold,
;;    returning the content upon hitting the closing asterisk"
;;   (declare (ignore char))
;;   (let ((content '()))
;;     (loop for ch = (read-char stream nil nil)
;;           while ch do
;;             (cond
;;               ((char= ch #\*)
;;                (return `(bold ,content)))
;;               (t
;;                (setf content (concatenate 'string content (string ch))))))))

;; Reader macros
;(defvar *previous-readtables* nil)
(eval-when (:compile-toplevel :load-toplevel :execute)
  (defvar *previous-readtables* '()
          "A stack to store previous readtables."))

(push *readtable* *previous-readtables*)

(defmacro enable-markdown-syntax ()
  `(eval-when (:compile-toplevel :load-toplevel :execute)
    (push *readtable* *previous-readtables*)
    (setq *readtable* (copy-readtable))
    (set-dispatch-macro-character #\# #\Space (make-header-reader 1))
    (set-dispatch-macro-character #\# #\# (make-header-reader 2))))
    ;(set-macro-character #\* #'read-bold)))

(defmacro disable-markdown-syntax ()
  `(eval-when (:compile-toplevel :load-toplevel :execute)
    (setq *readtable* (pop *previous-readtables*))))

;; HTML foo
(defun heading (level title)
  (format t "<h~a>~a</h~a>" level title level))

;; (defun bold (content)
;;   "Returns the HTML representation of the bold object"
;;   (format nil "<b>~a</b>" content))

(defun read-markdown-as-lisp (stream)
  (loop for char = (peek-char nil stream nil nil)
        while char
        collect (cond
                  ((member char '(#\( #\# #\'))
                   (read stream))
                  (t
                   (let ((line (read-line stream nil nil)))
                     (when line
                       `(par ,line)))))))


;; Glueing it all together
(progn
  (enable-markdown-syntax))
(format t "================~%")
(load "in/sport.md")
(disable-markdown-syntax)

(progn
  (enable-markdown-syntax)
  (format t "================~%")
  (load "in/other.md")
  (disable-markdown-syntax))

(progn
  (enable-markdown-syntax)
  (format t "================~%")
  (format t "~a~%" (with-open-file (stream "in/other.md"
                                          :direction :input)
                    (read-markdown-as-lisp stream)))
  (disable-markdown-syntax))
