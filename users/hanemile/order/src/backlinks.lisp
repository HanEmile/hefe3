(in-package :order)

(defun url-to-id (url)
  "Extract an object ID from an internal URL like /alice/ or /alice/#section."
  (when (and (stringp url) (> (length url) 0) (char= (char url 0) #\/))
    (let* ((hash-pos (position #\# url))
           (path (if hash-pos (subseq url 0 hash-pos) url))
           (section (when hash-pos (subseq url (1+ hash-pos))))
           (trimmed (string-trim "/" path)))
      (when (> (length trimmed) 0) (values trimmed section)))))

(defun extract-refs-from-inline (inline)
  (cond
    ((stringp inline) nil)
    ((and (listp inline) (eq (first inline) 'ref)) (list (rest inline)))
    ((and (listp inline) (eq (first inline) 'link))
     (multiple-value-bind (id section) (url-to-id (third inline))
       (when id (list (if section (list id section) (list id))))))
    ((listp inline) (mapcan #'extract-refs-from-inline inline))
    (t nil)))

(defun extract-refs (object)
  "Extract all reference targets from OBJECT's content."
  (mapcan (lambda (el) (when (listp el) (mapcan #'extract-refs-from-inline (rest el))))
          (object-content object)))

(defun build-backlinks (objects)
  "Scan all OBJECTS for cross-references and populate backlink lists."
  (let ((id-map (make-hash-table :test 'equal)))
    (dolist (o objects) (setf (gethash (object-id o) id-map) o))
    (dolist (source objects)
      (dolist (ref (extract-refs source))
        (let ((target (gethash (first ref) id-map)))
          (when target (add-backlink target (object-id source) (second ref))))))
    objects))
