(defpackage :custom-lib
  (:use :cl :iterate)
  (:export #:hello #:hello-iterate #:hello-borda-count))

(in-package :custom-lib)

(defun my-list ()
  (iter
    (for i from 1 to 3)
    (collect i)))

(defun hello (name)
  (format nil "Hello, ~A!" name))

(defun hello-iterate ()
  (format nil "Hello, ~S!" (my-list)))

(defun hello-borda-count (votes)
  (format nil "Hello, ~S!" (borda-count:borda-count votes)))
