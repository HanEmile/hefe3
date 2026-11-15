(load "markcl.lisp")

(defun process-file (filepath)
  "Reads a file using the document reader and returns a list of its parts."
  (let ((document-reader:*document-parts* nil))
    (handler-case
        (progn
          (document-reader:enable-document-reader)
          (with-open-file (stream filepath :direction :input)
            (loop for form = (read stream nil :eof)
                  until (eq form :eof)
                  do (push form document-reader:*document-parts*))))
      (finally
        (document-reader:disable-document-reader)))
    (nreverse document-reader:*document-parts*)))

;; --- Main execution ---

;; Process the file. This defines the functions (test, add) and
;; converts the markdown parts into Lisp code.
(let ((page-forms (process-file "test-page.lisp")))

  ;; Now, evaluate all the forms that were read from the file.
  ;; This will execute the defuns and the (concatenate 'string ...) forms.
  (let ((results (mapcar #'eval page-forms)))

    ;; Display the results
    ;; The results of `defun` are just the function names.
    ;; The results of text lines are the generated strings.
    (dolist (item results)
      (unless (or (null item) (symbolp item)) ; Filter out defun results and empty lines
        (print item)))))
