(in-package :order)

(defvar *object-db* nil)
(defvar *current-object* nil)
(defvar *build-visibility* "private")
(defvar *url-map* (make-hash-table :test 'equal))
(defvar *visibility-map* (make-hash-table :test 'equal))
(defvar *templates* (make-hash-table :test 'equal))

(defun resolve-url (id)
  (or (gethash id *url-map*) (format nil "/~A" id)))

(defun resolve-visibility (id)
  (gethash id *visibility-map* "public"))

(defun visible-in-build-p (id)
  (visibility-visible-p (resolve-visibility id) *build-visibility*))

(defun coerce-to-function (thing)
  (cond
    ((functionp thing) thing)
    ((null thing) nil)
    ((symbolp thing) (symbol-function thing))
    ((and (listp thing) (eq (first thing) 'lambda))
     (let ((*package* (find-package :order))) (eval thing)))
    ((and (listp thing) (symbolp (first thing)))
     (let* ((*package* (find-package :order)) (result (eval thing)))
       (if (functionp result) result (error "~S did not return a function" thing))))
    (t (error "Cannot coerce ~S to a function" thing))))

(defun execute-query (&key type visibility tags where sort sort-desc limit)
  (let* ((where-fn (coerce-to-function where))
         (results (remove-if-not
                   (lambda (o) (object-matches-p o :type type :visibility visibility
                                                   :tags tags :where where-fn))
                   *object-db*)))
    (when sort
      (setf results (sort (copy-list results) #'string<
                          :key (lambda (o) (or (object-property o sort) "")))))
    (when sort-desc
      (setf results (sort (copy-list results) #'string>
                          :key (lambda (o) (or (object-property o sort-desc) "")))))
    (when (and limit (numberp limit) (> limit 0))
      (setf results (subseq results 0 (min limit (length results)))))
    results))

(defun register-template (name function)
  (setf (gethash name *templates*) function))

(defmacro define-template (name (objects &rest args) &body body)
  `(register-template ,(string-downcase (string name))
                      (lambda (,objects &key ,@args &allow-other-keys) ,@body)))

(defun link-if-exists (text)
  "Render TEXT as a link if it has a URL in *url-map*, else return TEXT unchanged."
  (let ((url (gethash (sanitize text) *url-map*)))
    (if url (format nil "<a href=\"~A\">~A</a>" url text) text)))

(defun render-cell-value (val col object)
  (cond
    ((eq col :title) (format nil "<a href=\"~A\">~A</a>" (object-url object) (or val (object-id object))))
    ((or (null val) (string= val "")) "")
    ((position #\, val)
     (format nil "~{~A~^, ~}"
             (mapcar (lambda (p) (link-if-exists (string-trim '(#\Space #\Tab) p)))
                     (split-string val #\,))))
    (t (link-if-exists val))))

(defun emit-table (stream columns objects)
  (format stream "<table class=\"query-table\">~%<thead><tr>~%")
  (dolist (col columns) (format stream "  <th>~A</th>~%" (string-downcase (string col))))
  (format stream "</tr></thead>~%<tbody>~%")
  (dolist (o objects)
    (format stream "<tr>~%")
    (dolist (col columns)
      (format stream "  <td>~A</td>~%" (render-cell-value (object-property o col) col o)))
    (format stream "</tr>~%"))
  (format stream "</tbody></table>~%"))

(define-template list (objects show)
  (let ((show-props (or show '(:title))))
    (with-output-to-string (s)
      (format s "<ul class=\"query-list\">~%")
      (dolist (o objects)
        (format s "  <li><a href=\"~A\">~A</a>" (object-url o) (object-title o))
        (dolist (prop show-props)
          (unless (eq prop :title)
            (let ((val (object-property o prop)))
              (when val (format s " <span class=\"prop-~A\">~A</span>"
                                (string-downcase (string prop)) (render-cell-value val prop o))))))
        (format s "</li>~%"))
      (format s "</ul>~%"))))

(define-template table (objects columns)
  (with-output-to-string (s) (emit-table s (or columns '(:title :type :date)) objects)))

(defun extract-year (date-string)
  (if (and (>= (length date-string) 4) (every #'digit-char-p (subseq date-string 0 4)))
      (subseq date-string 0 4) date-string))

(defun render-grouped (objects group-fn show-props &key (sort-groups #'string>))
  (let ((groups (make-hash-table :test 'equal)) (group-keys nil)
        (cols (or show-props '(:title))))
    (dolist (o objects)
      (let ((key (funcall group-fn o)))
        (push o (gethash key groups))
        (pushnew key group-keys :test #'string=)))
    (setf group-keys (sort group-keys sort-groups))
    (with-output-to-string (s)
      (dolist (key group-keys)
        (format s "<h3>~A</h3>~%" (link-if-exists key))
        (emit-table s cols (gethash key groups))))))

(define-template group-by-date (objects group-by show)
  (render-grouped objects
                  (lambda (o) (extract-year (or (object-property o (or group-by :date)) "undated")))
                  (or show '(:title))))

(define-template timeline (objects show)
  (render-grouped objects
                  (lambda (o) (extract-year (or (object-property o :last-edited)
                                                (object-property o :date) "undated")))
                  (or show '(:title :type))))

(define-template group-by (objects by show)
  (render-grouped objects
                  (lambda (o) (or (object-property o (if by (to-keyword by) :type)) "other"))
                  (or show '(:title)) :sort-groups #'string<))

(defun run-query-from-args (args)
  (execute-query :type (getf args :type) :visibility (getf args :visibility)
                 :tags (getf args :tags) :where (getf args :where)
                 :sort (getf args :sort) :sort-desc (getf args :sort-desc)
                 :limit (getf args :limit)))

(defvar *direct-call-forms*
  '((graph . graph) (children . children) (trace-path . trace-path)
    (shelf-view . shelf-view) (count-objects . count-objects)))

(defvar *query-template-map*
  '((query . "list") (table . "table") (listing . "list")
    (group-by . "group-by") (group-by-date . "group-by-date") (timeline . "timeline")))

(defun eval-query-form (form)
  (let ((head (first form)) (args (rest form)))
    (cond
      ((eq head 'all-types) (all-types))
      ((eq head 'all-properties) (all-properties))
      ((assoc head *direct-call-forms*)
       (apply (symbol-function (cdr (assoc head *direct-call-forms*))) args))
      ((assoc head *query-template-map*)
       (let* ((tmpl (gethash (cdr (assoc head *query-template-map*)) *templates*))
              (results (run-query-from-args args)))
         (apply tmpl results args)))
      ((gethash (string-downcase (string head)) *templates*)
       (apply (gethash (string-downcase (string head)) *templates*)
              (run-query-from-args args) args))
      (t (format nil "<pre>Unknown: ~A</pre>" head)))))

(defun count-objects (&key type where tags)
  (let* ((where-fn (coerce-to-function where))
         (n (count-if (lambda (o) (object-matches-p o :type type :tags tags :where where-fn))
                      *object-db*)))
    (format nil "~A" n)))

(defun prop (property-name)
  (or (object-property *current-object* (intern (string-upcase property-name) :keyword)) ""))

(defun has (property value)
  (let ((key (intern (string-upcase property) :keyword)))
    (lambda (o) (let ((v (object-property o key))) (and v (search value v :test #'char-equal))))))

(defun types (&rest type-names)
  (lambda (o) (member (object-type o) type-names :test #'string-equal)))

(defun render-count-table (key-label count-label counter)
  (let ((pairs (sort (loop for k being the hash-keys of counter
                           collect (cons k (gethash k counter)))
                     #'string< :key #'car)))
    (with-output-to-string (s)
      (format s "<table class=\"query-table\">~%<thead><tr><th>~A</th><th>~A</th></tr></thead>~%<tbody>~%"
              key-label count-label)
      (dolist (pair pairs)
        (format s "<tr><td>~A</td><td>~A</td></tr>~%" (link-if-exists (car pair)) (cdr pair)))
      (format s "</tbody></table>~%"))))

(defun all-types ()
  (let ((counts (make-hash-table :test 'equal)))
    (dolist (o *object-db*) (let ((typ (object-type o))) (when typ (incf (gethash typ counts 0)))))
    (render-count-table "type" "count" counts)))

(defun all-properties ()
  (let ((counts (make-hash-table :test 'equal)))
    (dolist (o *object-db*)
      (loop for (key val) on (object-properties o) by #'cddr
            do (incf (gethash (string-downcase (string key)) counts 0))))
    (render-count-table "property" "used by" counts)))

(defun query-form-p (form)
  (and (listp form) (symbolp (first form))
       (or (member (first form) '(query table listing group-by graph children
                                   trace-path shelf-view count-objects all-types all-properties))
           (gethash (string-downcase (string (first form))) *templates*))))
