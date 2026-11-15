(in-package :order)

(defvar *cache-dir-name* ".order-cache")
(defvar *manifest-file-name* "manifest.sexp")

(defstruct cache-entry
  (mtime 0 :type integer)
  (refs nil :type list)
  (has-queries nil :type boolean)
  (props-hash 0 :type integer))

(defun manifest-path (output-dir)
  (merge-pathnames (concatenate 'string *cache-dir-name* "/" *manifest-file-name*) output-dir))

(defun load-manifest (output-dir)
  (let ((path (manifest-path output-dir)) (table (make-hash-table :test 'equal)))
    (when (probe-file path)
      (with-open-file (s path :direction :input :if-does-not-exist nil)
        (when s
          (let ((*package* (find-package :order)))
            (handler-case
                (let ((data (read s nil nil)))
                  (when (listp data)
                    (loop for (key mtime refs has-q props-h) in data
                          do (setf (gethash key table)
                                   (make-cache-entry :mtime mtime :refs refs
                                                     :has-queries has-q :props-hash props-h)))))
              (error () nil))))))
    table))

(defun save-manifest (output-dir manifest)
  (let ((path (manifest-path output-dir)))
    (ensure-directories-exist path)
    (with-open-file (s path :direction :output :if-exists :supersede)
      (write (let ((entries nil))
               (maphash (lambda (key entry)
                          (push (list key (cache-entry-mtime entry) (cache-entry-refs entry)
                                      (cache-entry-has-queries entry) (cache-entry-props-hash entry))
                                entries))
                        manifest)
               entries)
             :stream s :pretty nil))))

(defun object-has-queries-p (object)
  (if (some (lambda (el) (and (listp el) (query-form-p el))) (object-content object)) t nil))

(defun compute-object-refs (object)
  (mapcar #'first (extract-refs object)))

(defun build-cache-entry (object)
  (make-cache-entry
   :mtime (if (obj-path object) (or (file-write-date (obj-path object)) 0) 0)
   :refs (compute-object-refs object)
   :has-queries (object-has-queries-p object)
   :props-hash (sxhash (prin1-to-string (object-properties object)))))

(defun compute-dirty-set (objects old-manifest)
  "Compute which object IDs need re-rendering."
  (let ((dirty (make-hash-table :test 'equal)) (changed-ids nil)
        (id-map (make-hash-table :test 'equal)))
    (dolist (o objects) (setf (gethash (object-id o) id-map) o))
    ;; Phase 1: mtime changes + new + synthetic
    (dolist (o objects)
      (let* ((pk (if (obj-path o) (namestring (obj-path o)) (object-id o)))
             (cached (gethash pk old-manifest))
             (mt (if (obj-path o) (or (file-write-date (obj-path o)) 0) 0)))
        (when (or (null cached) (/= mt (cache-entry-mtime cached)) (null (obj-path o)))
          (push (object-id o) changed-ids) (setf (gethash (object-id o) dirty) t))))
    ;; Phase 2: pages linking TO a changed file
    (dolist (o objects)
      (unless (gethash (object-id o) dirty)
        (when (some (lambda (ref) (member ref changed-ids :test #'string-equal))
                    (compute-object-refs o))
          (setf (gethash (object-id o) dirty) t))))
    ;; Phase 3: pages a changed file links to (backlink changes)
    (dolist (cid changed-ids)
      (let ((o (gethash cid id-map)))
        (when o
          (let* ((pk (if (obj-path o) (namestring (obj-path o)) (object-id o)))
                 (cached (gethash pk old-manifest))
                 (all-refs (union (when cached (cache-entry-refs cached))
                                  (compute-object-refs o) :test #'string-equal)))
            (dolist (ref all-refs)
              (when (gethash ref id-map) (setf (gethash ref dirty) t)))))))
    ;; Phase 4: pages with queries
    (when changed-ids
      (dolist (o objects)
        (unless (gethash (object-id o) dirty)
          (when (object-has-queries-p o) (setf (gethash (object-id o) dirty) t)))))
    dirty))

(defun update-manifest (objects old-manifest)
  (declare (ignore old-manifest))
  (let ((new (make-hash-table :test 'equal)))
    (dolist (o objects)
      (setf (gethash (if (obj-path o) (namestring (obj-path o)) (object-id o)) new)
            (build-cache-entry o)))
    new))
