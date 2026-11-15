(in-package :order)

(defstruct obj
  (properties nil :type list)
  (content nil :type list)
  (path nil)
  (backlinks nil :type list)
  (url nil))

(defun make-object (path)
  "Create an object from the .md file at PATH."
  (multiple-value-bind (props content) (read-document path)
    (let ((o (make-obj :properties props :content content :path path)))
      (unless (object-property o :id)
        (setf (getf (obj-properties o) :id) (pathname-name path)))
      (unless (object-property o :visibility)
        (setf (getf (obj-properties o) :visibility) "private"))
      (unless (object-property o :type)
        (setf (getf (obj-properties o) :type) "page"))
      (unless (object-property o :last-edited)
        (let ((mtime (file-write-date path)))
          (when mtime
            (setf (getf (obj-properties o) :last-edited) (format-universal-time mtime)))))
      o)))

(defun object-property (object key)
  (getf (obj-properties object) key))

(defun (setf object-property) (value object key)
  (setf (getf (obj-properties object) key) value))

;; Generate trivial accessors: struct-slot wrappers and property wrappers
(macrolet ((def-slot-accessors (&rest pairs)
             `(progn ,@(loop for (name slot) on pairs by #'cddr
                             collect `(defun ,name (o) (,slot o)))))
           (def-prop-accessors (&rest pairs)
             `(progn ,@(loop for (name key) on pairs by #'cddr
                             collect `(defun ,name (o) (object-property o ,key))))))
  (def-slot-accessors
    object-properties obj-properties
    object-content    obj-content
    object-path       obj-path
    object-url        obj-url
    object-backlinks  obj-backlinks)
  (def-prop-accessors
    object-visibility :visibility
    object-type       :type
    object-id         :id))

(defun object-title (object)
  (or (object-property object :title) (object-property object :id)))

(defun add-backlink (object source-id &optional section)
  (pushnew (if section (list source-id section) (list source-id))
           (obj-backlinks object) :test #'equal))

(defun object-matches-p (object &key type visibility tags where)
  "Return T if OBJECT matches the given filter criteria."
  (and (or (null type) (string-equal (object-type object) type))
       (or (null visibility)
           (if (listp visibility)
               (member (object-visibility object) visibility :test #'string-equal)
               (string-equal (object-visibility object) visibility)))
       (or (null tags)
           (let ((obj-tags (split-string (or (object-property object :tags) "") #\,))
                 (match-tags (if (listp tags) tags (split-string tags #\,))))
             (intersection obj-tags match-tags :test
                           (lambda (a b)
                             (string-equal (string-trim '(#\Space) a)
                                           (string-trim '(#\Space) b))))))
       (or (null where) (funcall where object))))
