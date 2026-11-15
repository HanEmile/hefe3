;;; -*- Mode: LISP; Syntax: Common-lisp; Package: know; Lowercase: Yes -*-

(in-package :cl-knowledge)

(print "hi")

(print (- (+ 9000 900 90 9) (+ 5000 500 50 5)))

(print (append '(Pat Kim) '(Robin Sandy)))
(print (length (append '(Pat Kim) (list '(John Q Public) 'Sandy))))

(defvar p nil)
(print (setf p '(John Q Public)))
