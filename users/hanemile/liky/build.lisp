(load (sb-ext:posix-getenv "ASDF"))

(asdf:load-system "alexandria")
(asdf:load-system "custom-lib")
(asdf:load-system "document-templates")
(asdf:load-system "gdid")

(defun main ()
  (format t "Hello!~%")
  (format t "Hello alexandria: ~A~%" (alexandria:iota 10))
  (format t "Hello custom-lib: ~A~%" (custom-lib:hello "World"))
  (format t "Hello custom-lib using iterate: ~A~%" (custom-lib:hello-iterate))
  (format t "Hello custom-lib using borda-count: ~A~%" (custom-lib:hello-borda-count '((a b c))))
  (format t "Version of document-templates: ~A~%" (asdf:system-version (asdf:find-system :document-templates)))
  (format t "Version of gdid: ~A~%" (asdf:system-version (asdf:find-system :gdid))))

(sb-ext:save-lisp-and-die
 "app"
 :toplevel #'main
 :executable t)
