(in-package :order)

(defstruct render-state
  (h (vector 0 0 0 0 0) :type simple-vector)
  (headings nil))

(defun visibility-css-attr (vis &optional (prefix "link"))
  "Return an HTML class attribute string for visibility, or empty for public/nil."
  (cond ((and vis (string-equal vis "private")) (format nil " class=\"~A-private\"" prefix))
        ((and vis (string-equal vis "internal")) (format nil " class=\"~A-internal\"" prefix))
        (t "")))

(defun render-inline (inline)
  "Render inline content to HTML string."
  (cond
    ((null inline) "")
    ((stringp inline) (escape-html inline))
    ((and (listp inline) (symbolp (first inline)))
     (let ((tag (first inline)))
       (case tag
         (ref (let* ((page (second inline)) (section (third inline))
                     (url (resolve-url page)) (vis (resolve-visibility page)))
                (if (not (visible-in-build-p page))
                    (if section (format nil "~A#~A" page section) page)
                    (let ((cls (visibility-css-attr vis)))
                      (if section
                          (format nil "<a~A href=\"~A#~A\">~A#~A</a>" cls url (sanitize section) page section)
                          (format nil "<a~A href=\"~A\">~A</a>" cls url page))))))
         (link (let* ((url (third inline)) (text (second inline))
                      (target-id (multiple-value-bind (id) (url-to-id url) id))
                      (vis (when target-id (resolve-visibility target-id))))
                 (if (and target-id (not (visible-in-build-p target-id))) text
                     (format nil "<a~A href=\"~A\">~A</a>" (visibility-css-attr vis) url text))))
         (code (format nil "<code>~A</code>" (render-inline (second inline))))
         (bold (format nil "<strong>~A</strong>" (render-inline (second inline))))
         (italic (format nil "<em>~A</em>" (render-inline (second inline))))
         (img (format nil "<figure><img src=\"~A\" alt=\"~A\"><figcaption>~A</figcaption></figure>"
                      (third inline) (second inline) (second inline)))
         (otherwise (format nil "~{~A~}" (mapcar #'render-inline (rest inline)))))))
    ((listp inline) (format nil "~{~A~}" (mapcar #'render-inline inline)))
    (t (format nil "~A" inline))))

(defun heading-number (state level)
  (let ((h (render-state-h state)))
    (format nil "~{~A~^.~}" (loop for i below level collect (aref h i)))))

(defun bump-heading (state level)
  (let ((h (render-state-h state)))
    (incf (aref h (1- level)))
    (loop for i from level below 5 do (setf (aref h i) 0))))

(defun heading-anchor (content)
  (sanitize (if (stringp content) content (format nil "~A" content))))

(defun render-heading (state level content)
  (bump-heading state level)
  (let* ((text (render-inline content)) (num (heading-number state level))
         (anchor (heading-anchor content)))
    (push (list level num text anchor) (render-state-headings state))
    (format nil "<span id=\"~A\"></span>~%<h~A><a href=\"#~A\">~A. ~A</a></h~A>"
            anchor level anchor num text level)))

(defun render-toc-sidebar (object)
  (let ((toc-state (make-render-state)) (skip-depth 0))
    (dolist (element (object-content object))
      (cond
        ((and (listp element) (eq (first element) 'vis-start))
         (unless (visibility-allows-p (second element)) (incf skip-depth)))
        ((and (listp element) (eq (first element) 'vis-end))
         (when (> skip-depth 0) (decf skip-depth)))
        ((and (= skip-depth 0) (listp element) (member (first element) '(h1 h2 h3 h4 h5)))
         (let ((level (1+ (position (first element) #(h1 h2 h3 h4 h5)))))
           (bump-heading toc-state level)
           (push (list level (heading-number toc-state level)
                       (render-inline (second element)) (heading-anchor (second element)))
                 (render-state-headings toc-state))))))
    (let ((headings (nreverse (render-state-headings toc-state))))
      (if headings
          (with-output-to-string (s)
            (format s "<div class=\"toc\"><pre>~%")
            (dolist (h headings)
              (destructuring-bind (level num text anchor) h
                (format s "~A<a href=\"#~A\">~A. ~A</a>~%"
                        (make-string (* (1- level) 3) :initial-element #\Space) anchor num text)))
            (format s "</pre></div>~%"))
          ""))))

(defun toc () "")

(defun render-backlinks (object)
  (let ((bls (remove-if-not (lambda (bl) (visible-in-build-p (first bl)))
                            (object-backlinks object))))
    (if (null bls) ""
        (with-output-to-string (s)
          (format s "<h2>Backlinks</h2>~%<ul class=\"backlinks\">")
          (dolist (bl bls)
            (let* ((id (first bl)) (section (second bl)) (url (resolve-url id))
                   (cls (visibility-css-attr (resolve-visibility id) "bl")))
              (if section
                  (format s "<li~A><a href=\"~A#~A\">~A#~A</a></li>" cls url (sanitize section) id section)
                  (format s "<li~A><a href=\"~A\">~A</a></li>" cls url id))))
          (format s "</ul>")))))

(defun render-element (element state object)
  "Render ELEMENT. Returns (values html :inline-or-:block)."
  (when (null element) (return-from render-element (values "" :inline)))
  (let ((tag (if (listp element) (first element) nil))
        (content (if (listp element) (second element) nil)))
    (case tag
      (blank (values (format nil "~%") :inline))
      (hr (values "<hr>" :block))
      ((h1 h2 h3 h4 h5)
       (values (render-heading state (1+ (position tag #(h1 h2 h3 h4 h5))) content) :block))
      (blockquote (values (format nil "<pre class=\"code\">~A</pre>~%" (render-inline content)) :block))
      (li (values (format nil "- ~A~%" (render-inline content)) :inline))
      (ol-item (values (format nil "~A. ~A~%" content (render-inline (third element))) :inline))
      (code-block
       (let ((lang content) (code (third element)))
         (values (if (and lang (> (length lang) 0))
                     (format nil "<pre class=\"code\" data-lang=\"~A\">~A</pre>~%" (render-inline lang) (render-inline code))
                     (format nil "<pre class=\"code\">~A</pre>~%" (render-inline code)))
                 :block)))
      (text (if (and (listp content) (eq (first content) 'img))
                (values (format nil "~A~%" (render-inline content)) :block)
                (values (format nil "~A~%" (render-inline content)) :inline)))
      (directive
       (cond ((string-equal content "toc") (values "" :inline))
             ((string-equal content "backlinks")
              (let ((html (render-backlinks object)))
                (if (string= html "") (values "" :inline) (values html :block))))
             (t (values (format nil "[unknown directive: ~A]~%" content) :inline))))
      (otherwise
       (if (and (listp element) (query-form-p element))
           (values (eval-query-form element) :block)
           (if (listp element)
               (handler-case
                   (let* ((*package* (find-package :order)) (result (eval element)))
                     (if (or (null result) (and (stringp result) (zerop (length result))))
                         (values "" :inline)
                         (values (format nil "~A~%" result) :inline)))
                 (error (e) (values (format nil "<span class=\"eval-error\">(~A: ~A)</span>~%" (first element) e) :inline)))
               (values (format nil "~A~%" (render-inline element)) :inline)))))))

(defun visibility-allows-p (required-level)
  (visibility-visible-p required-level *build-visibility*))

(defun render-object-content (object)
  (let ((state (make-render-state)) (*current-object* object)
        (skip-depth 0) (skip-next-blank nil) (pre-open nil))
    (with-output-to-string (s)
      (flet ((open-pre () (unless pre-open (write-string "<pre>" s) (setf pre-open t)))
             (close-pre () (when pre-open (write-string "</pre>" s) (setf pre-open nil))))
        (dolist (element (object-content object))
          (cond
            ((and (listp element) (eq (first element) 'vis-start))
             (let ((level (second element)))
               (if (visibility-allows-p level)
                   (progn (close-pre) (format s "<div class=\"visibility-~A\">" level))
                   (incf skip-depth)))
             (setf skip-next-blank t))
            ((and (listp element) (eq (first element) 'vis-end))
             (if (> skip-depth 0) (decf skip-depth)
                 (progn (close-pre) (write-string "</div>" s)))
             (setf skip-next-blank t))
            ((and skip-next-blank (listp element) (eq (first element) 'blank))
             (setf skip-next-blank nil))
            ((= skip-depth 0)
             (setf skip-next-blank nil)
             (multiple-value-bind (html kind) (render-element element state object)
               (when (> (length html) 0)
                 (if (eq kind :block) (progn (close-pre) (write-string html s))
                     (progn (open-pre) (write-string html s))))))
            (t (setf skip-next-blank nil))))
        (let ((bl (render-backlinks object)))
          (when (> (length bl) 0) (close-pre) (write-string bl s)))
        (close-pre)))))

(defun render-nav (object base-path)
  (if (null (object-path object))
      (format nil "<nav>~%  <ul>~%    <li><a href=\"~A\">~A</a></li>~%  </ul>~%</nav>~%"
              (resolve-url (object-type object)) (object-type object))
      (let* ((rel (relative-path (object-path object) base-path))
             (components (path-components (directory-namestring rel))))
        (with-output-to-string (s)
          (format s "<nav>~%  <ul>~%")
          (let ((path-so-far ""))
            (dolist (comp components)
              (setf path-so-far (concatenate 'string path-so-far comp "/"))
              (format s "    <li><a href=\"/~A\">~A</a></li>~%" path-so-far comp)))
          (format s "  </ul>~%  <ul style=\"float: right\">~%    <li><a href=\"~A\">.md</a></li>~%  </ul>~%</nav>~%"
                  (file-namestring (object-path object)))))))

(defun render-meta-date-parts (text)
  (let ((y (subseq text 0 4)) (m (subseq text 5 7)) (d (subseq text 8 10)))
    (format nil "<a href=\"~A\">~A</a>-<a href=\"~A\">~A</a>-<a href=\"~A\">~A</a>"
            (resolve-url y) y (resolve-url (format nil "~A-~A" y m)) m (resolve-url text) d)))

(defun render-meta-atom (text)
  (let ((trimmed (string-trim '(#\Space #\Tab) text)))
    (if (date-string-p trimmed) (render-meta-date-parts trimmed) (link-if-exists trimmed))))

(defun render-meta-value (val)
  "Render a property value. Handles comma-separated lists and colon-separated sub-parts."
  (flet ((render-colon (text)
           (let ((parts (split-string text #\:)))
             (if (= (length parts) 1) (render-meta-atom text)
                 (format nil "~{~A~^:~}" (mapcar #'render-meta-atom parts))))))
    (format nil "~{~A~^, ~}"
            (mapcar (lambda (p) (render-colon (string-trim '(#\Space #\Tab) p)))
                    (split-string val #\,)))))

(defvar *context-link-properties* '(:shelf :part-of))

(defun render-meta-item (key val obj-id)
  (let ((key-str (string-downcase (string key))))
    (if (member key *context-link-properties*)
        (let ((url (gethash (sanitize val) *url-map*)))
          (if url
              (format nil "  <div class=\"meta-item\"><strong>~A</strong> <a href=\"~A#~A\">~A ↗</a></div>~%" key-str url obj-id val)
              (format nil "  <div class=\"meta-item\"><strong>~A</strong> ~A</div>~%" key-str (render-meta-value val))))
        (format nil "  <div class=\"meta-item\"><strong>~A</strong> ~A</div>~%" key-str (render-meta-value val)))))

(defun render-properties-meta (object)
  (let ((props (object-properties object)) (skip '(:title :id)) (obj-id (object-id object)))
    (with-output-to-string (s)
      (let ((items (loop for (key val) on props by #'cddr
                         unless (member key skip)
                         when (and val (stringp val) (> (length val) 0))
                         collect (render-meta-item key val obj-id))))
        (when items
          (format s "<div class=\"object-meta\">~%")
          (dolist (item items) (write-string item s))
          (format s "</div>~%"))))))

(defun render-to-html (object &key site-name style base-path)
  (with-output-to-string (s)
    (format s "<!DOCTYPE html>~%<html lang=\"en\">~%<head>~%  <meta charset=\"UTF-8\">~%  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1.0\">~%  <title>~A - ~A</title>~%"
            (object-title object) (or site-name "order"))
    (when style (format s "  <style>~%~A~%  </style>~%" style))
    (format s "</head>~%<body class=\"page-~A\">~%" (or (object-visibility object) "private"))
    (format s "<header>~%  <a href=\"/\">~A</a>~%  <input type=\"text\" id=\"search\" placeholder=\"search...\" autocomplete=\"off\">~%  <div id=\"search-results\"></div>~%</header>~%"
            (or site-name "order"))
    (when base-path (write-string (render-nav object base-path) s))
    (let ((*current-object* object))
      (write-string (render-toc-sidebar object) s))
    (write-string (render-properties-meta object) s)
    (write-string (render-object-content object) s)
    (format s "<br><br><br>~%<footer>~%  <pre>generated by <a href=\"https://github.com/hanemile/order\">order</a> ~A</pre>~%</footer>~%"
            (multiple-value-bind (sec min hour day month year) (get-decoded-time)
              (format nil "~4,'0D-~2,'0D-~2,'0D ~2,'0D:~2,'0D:~2,'0D" year month day hour min sec)))
    (write-string "<script>
(function(){
var d=null,i=document.getElementById('search'),r=document.getElementById('search-results');
var matches=[];
function hide(){r.innerHTML='';r.classList.remove('active');matches=[]}
function doSearch(){
  var q=i.value.toLowerCase();matches=[];
  if(q.length<2){hide();return}
  if(!d){fetch('/search.json').then(function(x){return x.json()}).then(function(j){d=j;doSearch()});return}
  var h='';for(var x=0;x<d.length&&matches.length<15;x++){
    var o=d[x];if(o.t.toLowerCase().indexOf(q)>=0||o.s.toLowerCase().indexOf(q)>=0||o.y.toLowerCase().indexOf(q)>=0||o.p.toLowerCase().indexOf(q)>=0){
      matches.push(o);h+='<a href=\"'+o.u+'\">'+o.t+' <small>'+o.y+'</small></a>'}}
  r.innerHTML=matches.length?h:'<small>no results</small>';r.classList.add('active')}
i.addEventListener('input',doSearch);
i.addEventListener('keydown',function(e){
  if(e.key==='Escape'){i.value='';hide();i.blur()}
  if(e.key==='Enter'){e.preventDefault();if(matches.length)window.location=matches[0].u}
});
document.addEventListener('click',function(e){if(!r.contains(e.target)&&e.target!==i)hide()});
})();
</script>
</body>
</html>
" s)))
