(in-package :vokobe3)

(defun list-tags-inline ()
  (let ((all nil)) ;; Fix: Removed the extra parens around nil
    (maphash (lambda (k v) 
               (declare (ignore k)) ;; Fix: Clean up style warning
               (setf all (union all v :test #'string-equal))) 
             *tags*)
    (if all
        (format nil "~{~A~^, ~}" (sort all #'string<))
        "none")))

(defun link (name)
  (format nil "<a href='~A.html'>~A</a>" name name))
