# The main title

(disable-markdown-syntax)
(defun toc ()
  (let* ((a "Table")
         (b a))
    (format t "~a~%" b)
    (format nil "~a~%" "Table Of Contents")))
(enable-markdown-syntax)

(princ (toc))

(format t "abc")
