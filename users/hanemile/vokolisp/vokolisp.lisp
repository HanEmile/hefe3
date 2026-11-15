(print (+ 1 2))xsxs

(print (concatenate 'string "so" "this" "is"))

; C-c C-d C-h -> common lisp hyperspace lookup

(print (open "vokolisp.lisp" :direction :input :if-does-not-exist :create))

; --

(defun make-blob (title content)
  (list :title title :content content))

(defun add-record (blob)
  (push blob *db*))

(defun prompt-read (prompt)
  (format *query-io* "~a: " prompt)
  (force-output *query-io*)
  (read-line *query-io*))
f
(defun prompt-for-blob ()x
  (make-blob
   (prompt-read "Title")
   (prompt-read "Content")))

(defun add-blobs ()
  (loop (add-record (prompt-for-blob))
        (if (not (y-or-n-p "Another?"))
            (return))))

; database
(defvar *db* nil)

(defun dump-db ()
  (dolist (post *db*)
    (format t "~{~a:~10t~a~%~}~%" post)))

;;; Save the *db* to the file with the given filename
(defun save-db (filename)
  (with-open-file
      (out filename
           :direction :output
           :if-exists :supersede)
    (with-standard-io-syntax
      (print *db* out))))

;;; Load the *db* using the content from the file with the given filename
(defun load-db (filename)
  (with-open-file (in filename)
    (with-standard-io-syntax
      (setf *db* (read in)))))

(defun update (selector-fn &key title content)
  (setf *db*
        (mapcar
         #'(lambda (row)
             (when (funcall selector-fn row)
               (if title (setf (getf row :title) title))
               (if content (setf (getf row :content) content)))
             row) *db*)))

(defun delete-row (selector-fn)
  (setf *db* (remove-if selector-fn *db*)))

(defun title-selector (title)
  #'(lambda (blob) (equal (getf blob :title) title)))

(defun select (selector-fn)
  (remove-if-not selector-fn *db*))

(print (select (title-selector "qwe")))

(make-blob "title-a" "content-a")

;; ---

(defconstant +hashtag+ #\#)
(defconstant +newline+ #\n)

(defun read-heading (end-char &optional (input-stream *standard-input*))
  (flet ((peek-next-char () (peek-char t input-stream t nil t))
         (discard-next-char () (read-char input-stream t nil t)))
:q    (if (char= (peek-next-char) end-char)
        (nil)
        (let* ((object (read input-stream t nil t))
               (next-char (peek-next-char)))
          (cond ((char= next-char end-char) (discard-next-char))
                object)))))

(defun read-hashtag (stream char)
  (declare (ignore char))
  (let ((*readtable* (copy-readtable)))
    (set-macro-character +hashtag+ 'read-start)
    (set-macro-character +newline+ 'read-end)
    (loop for object = (read-heading +newline+ stream)
          while object
          collect object into objects
          finally (return `(vector ,@objects)))))

; used as a stack of readtables for allowing the user to dynamically enable and disable the functionality
(defvar *previous-readtables* nil)

(defmacro enable-md-syntax ()
  '(eval-when (:compile-toplevel :load-toplevel :execute)
    (push *readtable* *previous-readtables*)
    (setq *readtable* (copy-readtable))
    (set-macro-character +hashtag+ 'read-hashtag)
    (set-macro-character +brace+ 'read-delimiter)))

(defmacro disable-md-syntax ()
  '(eval-when (:compile-toplevel :load-toplevel :execute)
    (setq *readtable* (pop *previous-readtables*))))

