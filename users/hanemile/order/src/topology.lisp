(in-package :order)

(defstruct topo-node
  id title url type (children nil) (x 0) (y 0) (width 0))

(defstruct topo-edge
  label1 label2 target)

(defun parse-edge-property (value)
  "Parse comma-separated edge property into list of (target-id label1 label2)."
  (when (and value (stringp value) (> (length value) 0))
    (mapcar (lambda (entry)
              (let ((parts (split-string (string-trim '(#\Space #\Tab) entry) #\:)))
                (list (first parts) (or (second parts) "") (or (third parts) ""))))
            (split-string value #\,))))

(defun build-graph-tree (root-id edge-prop &key (depth 10))
  (let ((visited (make-hash-table :test 'equal)))
    (labels ((build (id remaining)
               (when (and (> remaining 0) (not (gethash id visited)))
                 (setf (gethash id visited) t)
                 (let ((obj (find id *object-db* :key #'object-id :test #'string-equal)))
                   (when obj
                     (let* ((node (make-topo-node :id id :title (object-title obj)
                                                  :url (object-url obj) :type (object-type obj)))
                            (edges (parse-edge-property
                                    (object-property obj (intern (string-upcase edge-prop) :keyword))))
                            (kids nil))
                       (dolist (ed edges)
                         (destructuring-bind (tid l1 l2) ed
                           (let ((child (build tid (1- remaining))))
                             (when child (push (make-topo-edge :label1 l1 :label2 l2 :target child) kids)))))
                       (setf (topo-node-children node) (nreverse kids))
                       node))))))
      (build root-id depth))))

(defparameter *node-w* 140)
(defparameter *node-h* 40)
(defparameter *h-gap* 20)
(defparameter *v-gap* 60)
(defparameter *max-svg-w* 780)
(defparameter *port-font-size* 10)
(defparameter *label-font-size* 11)

(defun leaf-width (edge)
  (let ((child (topo-edge-target edge)))
    (if (null (topo-node-children child)) *node-w*
        (max *node-w* (reduce #'+ (mapcar #'leaf-width (topo-node-children child))
                              :initial-value (* (1- (length (topo-node-children child))) *h-gap*))))))

(defun subtree-height (node)
  (let ((max-y (+ (topo-node-y node) *node-h*)))
    (dolist (edge (topo-node-children node))
      (setf max-y (max max-y (subtree-height (topo-edge-target edge)))))
    (- max-y (topo-node-y node))))

(defun layout-tree (node x y)
  (let ((children (topo-node-children node)))
    (if (null children)
        (progn (setf (topo-node-x node) x (topo-node-y node) y (topo-node-width node) *node-w*) *node-w*)
        (let ((rows nil) (cur-row nil) (cur-w 0))
          (dolist (edge children)
            (let ((est-w (+ (leaf-width edge) *h-gap*)))
              (when (and cur-row (> (+ cur-w est-w) (+ *max-svg-w* *h-gap*)))
                (push (nreverse cur-row) rows) (setf cur-row nil cur-w 0))
              (push edge cur-row) (incf cur-w est-w)))
          (when cur-row (push (nreverse cur-row) rows))
          (setf rows (nreverse rows))
          (let ((max-rw 0) (cy (+ y *node-h* *v-gap*)))
            (dolist (row rows)
              (let ((cx x) (rw 0) (rh 0))
                (dolist (edge row)
                  (let ((cw (layout-tree (topo-edge-target edge) cx cy)))
                    (setf rh (max rh (subtree-height (topo-edge-target edge))))
                    (incf cx (+ cw *h-gap*)) (incf rw (+ cw *h-gap*))))
                (setf max-rw (max max-rw (- rw *h-gap*)))
                (incf cy (+ rh *v-gap*))))
            (let ((tw (max *node-w* max-rw)))
              (setf (topo-node-x node) (+ x (floor (- tw *node-w*) 2))
                    (topo-node-y node) y (topo-node-width node) tw)
              tw))))))

(defun layout-tree-horizontal (node x y)
  (let ((children (topo-node-children node)))
    (if (null children)
        (progn (setf (topo-node-x node) x (topo-node-y node) y (topo-node-width node) *node-h*) *node-h*)
        (let ((cy y) (th 0))
          (dolist (edge children)
            (let ((ch (layout-tree-horizontal (topo-edge-target edge) (+ x *node-w* *v-gap*) cy)))
              (incf cy (+ ch *h-gap*)) (incf th (+ ch *h-gap*))))
          (setf th (max (- th *h-gap*) *node-h*))
          (setf (topo-node-x node) x (topo-node-y node) (+ y (floor (- th *node-h*) 2))
                (topo-node-width node) th)
          th))))

(defun tree-is-linear-p (node)
  (and (<= (length (topo-node-children node)) 1)
       (every (lambda (e) (tree-is-linear-p (topo-edge-target e))) (topo-node-children node))))

(defun count-tree-nodes (node)
  (1+ (reduce #'+ (mapcar (lambda (e) (count-tree-nodes (topo-edge-target e)))
                           (topo-node-children node)) :initial-value 0)))

(defun svg-escape (str) (escape-html str t))

(defun truncate-for-svg (text max-chars)
  (if (<= (length text) max-chars) text
      (concatenate 'string (subseq text 0 (- max-chars 1)) "…")))

(defun render-topo-node-svg (node s)
  (let* ((x (topo-node-x node)) (y (topo-node-y node))
         (title (svg-escape (truncate-for-svg (topo-node-title node) 18)))
         (typ (svg-escape (or (topo-node-type node) ""))))
    (format s "<a href=\"~A\">~%  <rect x=\"~A\" y=\"~A\" width=\"~A\" height=\"~A\" rx=\"4\" class=\"topo-box\"/>~%  <text x=\"~A\" y=\"~A\" class=\"topo-title\">~A</text>~%"
            (topo-node-url node) x y *node-w* *node-h* (+ x (floor *node-w* 2)) (+ y 16) title)
    (when (> (length typ) 0)
      (format s "  <text x=\"~A\" y=\"~A\" class=\"topo-type\">~A</text>~%" (+ x (floor *node-w* 2)) (+ y 30) typ))
    (format s "</a>~%")))

(defun edge-label (edge)
  (let ((l1 (topo-edge-label1 edge)) (l2 (topo-edge-label2 edge)))
    (cond ((and l1 (> (length l1) 0) l2 (> (length l2) 0))
           (format nil "~A · ~A" (svg-escape l1) (svg-escape l2)))
          ((and l1 (> (length l1) 0)) (svg-escape l1))
          ((and l2 (> (length l2) 0)) (svg-escape l2)))))

(defun emit-edge (s path-d px py cx cy label lx ly)
  (format s "<path d=\"~A\" class=\"topo-edge\"/>~%<circle cx=\"~A\" cy=\"~A\" r=\"3\" class=\"topo-dot\"/>~%<circle cx=\"~A\" cy=\"~A\" r=\"3\" class=\"topo-dot\"/>~%"
          path-d px py cx cy)
  (when label (format s "<text x=\"~A\" y=\"~A\" class=\"topo-bw\">~A</text>~%" lx ly label)))

(defun render-topo-edge-svg (parent edge s hp)
  (let* ((child (topo-edge-target edge)) (label (edge-label edge)))
    (if hp
        (let* ((px (+ (topo-node-x parent) *node-w*)) (py (+ (topo-node-y parent) (floor *node-h* 2)))
               (cx (topo-node-x child)) (cy (+ (topo-node-y child) (floor *node-h* 2)))
               (mx (floor (+ px cx) 2)))
          (emit-edge s (format nil "M ~A ~A H ~A V ~A H ~A" px py mx cy cx)
                     px py cx cy label (+ mx 3) (- (floor (+ py cy) 2) 4)))
        (let* ((px (+ (topo-node-x parent) (floor *node-w* 2))) (py (+ (topo-node-y parent) *node-h*))
               (cx (+ (topo-node-x child) (floor *node-w* 2))) (cy (topo-node-y child))
               (my (floor (+ py cy) 2)))
          (emit-edge s (format nil "M ~A ~A V ~A H ~A V ~A" px py (+ py (min 20 (floor (- cy py) 2))) cx cy)
                     px py cx cy label (+ (floor (+ px cx) 2) 4) (- my 4))))))

(defun render-topo-tree-svg (node s hp)
  (dolist (edge (topo-node-children node))
    (render-topo-edge-svg node edge s hp)
    (render-topo-tree-svg (topo-edge-target edge) s hp))
  (render-topo-node-svg node s))

(defun compute-tree-bounds (node)
  (let ((mx (+ (topo-node-x node) *node-w*)) (my (+ (topo-node-y node) *node-h*)))
    (dolist (edge (topo-node-children node))
      (destructuring-bind (cmx cmy) (compute-tree-bounds (topo-edge-target edge))
        (setf mx (max mx cmx) my (max my cmy))))
    (list mx my)))

(defvar *topo-css* (format nil
  ".topo-box{fill:none;stroke:currentColor;stroke-width:1.5}~
   .topo-edge{fill:none;stroke:currentColor;stroke-width:1;opacity:.6}~
   .topo-dot{fill:currentColor;opacity:.4}~
   .topo-title{font-family:monospace;font-size:~Apx;fill:currentColor;text-anchor:middle;font-weight:bold}~
   .topo-type{font-family:monospace;font-size:~Apx;fill:currentColor;text-anchor:middle;opacity:.6}~
   .topo-port{font-family:monospace;font-size:~Apx;fill:currentColor;opacity:.5}~
   .topo-bw{font-family:monospace;font-size:~Apx;fill:currentColor;font-weight:bold;opacity:.7}~
   .topo-svg a:hover .topo-box{stroke-width:2.5}"
  *label-font-size* *port-font-size* *port-font-size* *port-font-size*))

(defun render-svg-wrapper (tree)
  (let ((hp (and (tree-is-linear-p tree) (<= (count-tree-nodes tree) 5))))
    (if hp (layout-tree-horizontal tree 10 10) (layout-tree tree 10 10))
    (destructuring-bind (mx my) (compute-tree-bounds tree)
      (let ((w (+ mx 10)) (h (+ my 10)))
        (with-output-to-string (s)
          (format s "<svg class=\"topo-svg\" viewBox=\"0 0 ~A ~A\" width=\"100%\" style=\"max-width:~Apx\" xmlns=\"http://www.w3.org/2000/svg\">~%<style>~A</style>~%"
                  w h w *topo-css*)
          (render-topo-tree-svg tree s hp)
          (format s "</svg>~%"))))))

(defun graph (root-id &key (edge "connects-to") (depth 10))
  (let ((tree (build-graph-tree root-id edge :depth depth)))
    (if tree (render-svg-wrapper tree)
        (format nil "<span class=\"eval-error\">graph: ~A not found</span>" root-id))))

(defun to-keyword (sym)
  (if (keywordp sym) sym (intern (string-upcase (string sym)) :keyword)))

(defun children (parent-id &key (via "part-of") (show '(:title :type)))
  (let* ((prop-key (intern (string-upcase via) :keyword))
         (columns (mapcar #'to-keyword show))
         (parts (remove-if-not
                 (lambda (o) (string-equal (or (object-property o prop-key) "") parent-id))
                 *object-db*)))
    (if (null parts) ""
        (with-output-to-string (s) (emit-table s columns parts)))))

(defun trace-path (from-id to-id &key (edge "connects-to"))
  (let* ((prop-key (intern (string-upcase edge) :keyword))
         (adj (make-hash-table :test 'equal))
         (visited (make-hash-table :test 'equal))
         (parent (make-hash-table :test 'equal))
         (queue nil))
    (dolist (o *object-db*)
      (dolist (e (parse-edge-property (object-property o prop-key)))
        (destructuring-bind (target l1 l2) e
          (push (list target l1 l2) (gethash (object-id o) adj))
          (push (list (object-id o) l1 l2) (gethash target adj)))))
    (setf (gethash from-id visited) t queue (list from-id))
    (loop while queue do
      (let ((cur (pop queue)))
        (when (string-equal cur to-id)
          (let ((path nil) (id to-id))
            (loop while id do
              (let ((p (gethash id parent)))
                (if p (progn (push (list id (second p) (third p)) path) (setf id (first p)))
                    (progn (push (list id "" "") path) (setf id nil)))))
            (return-from trace-path (build-trace-svg path))))
        (dolist (nb (gethash cur adj))
          (destructuring-bind (nid l1 l2) nb
            (unless (gethash nid visited)
              (setf (gethash nid visited) t (gethash nid parent) (list cur l1 l2)
                    queue (append queue (list nid))))))))
    (format nil "<span class=\"eval-error\">trace: no path from ~A to ~A</span>" from-id to-id)))

(defun build-trace-svg (hops)
  (let* ((nodes (mapcar (lambda (hop)
                          (let* ((id (first hop))
                                 (obj (find id *object-db* :key #'object-id :test #'string-equal)))
                            (make-topo-node :id id
                             :title (if obj (object-title obj) id)
                             :url (if obj (object-url obj) (format nil "/~A/" id))
                             :type (if obj (object-type obj) ""))))
                        hops))
         (root (first nodes)))
    (loop for (node next) on nodes for hop in hops when next
          do (setf (topo-node-children node)
                   (list (make-topo-edge :label1 (second hop) :label2 (third hop) :target next))))
    (render-svg-wrapper root)))

(defun shelf-view (shelf-id &key (cols 4) (rows 10))
  (let* ((boxes (remove-if-not
                 (lambda (o) (and (string-equal (object-type o) "box")
                                  (string-equal (or (object-property o :shelf) "") shelf-id)))
                 *object-db*))
         (cw 140) (ch 36) (pad 4) (lh 18)
         (svg-w (+ lh (* cols (+ cw pad)) pad))
         (svg-h (+ lh (* rows (+ ch pad)) pad))
         (grid (make-hash-table :test 'equal))
         (covered (make-hash-table :test 'equal))
         (warnings nil))
    (dolist (box boxes)
      (let ((rs (object-property box :shelf-row)) (cs (object-property box :shelf-col))
            (ss (or (object-property box :shelf-span) "1")))
        (when (and rs cs)
          (let ((row (parse-integer rs :junk-allowed t)) (col (parse-integer cs :junk-allowed t))
                (span (or (parse-integer ss :junk-allowed t) 1)) (id (object-id box)))
            (when (and row col)
              (when (or (< row 1) (> row rows))
                (push (format nil "~A: row ~A out of bounds (1-~A)" id row rows) warnings))
              (when (or (< col 1) (> (+ col span -1) cols))
                (push (format nil "~A: col ~A span ~A out of bounds (1-~A)" id col span cols) warnings))
              (loop for c from col below (+ col span)
                    for existing = (or (gethash (cons row c) grid) (gethash (cons row c) covered))
                    when existing do (push (format nil "~A: collision at row ~A col ~A (occupied by ~A)"
                                                  id row c (if (typep existing 'obj) (object-id existing) "span"))
                                           warnings))
              (setf (gethash (cons row col) grid) box)
              (loop for c from (1+ col) below (+ col span) do (setf (gethash (cons row c) covered) t)))))))
    (with-output-to-string (s)
      (format s "<svg class=\"shelf-svg\" viewBox=\"0 0 ~A ~A\" width=\"100%\" style=\"max-width:~Apx\" xmlns=\"http://www.w3.org/2000/svg\">~%" svg-w svg-h svg-w)
      (write-string "<style>.shelf-cell{fill:none;stroke:currentColor;stroke-width:1;opacity:.3}.shelf-box{fill:none;stroke:currentColor;stroke-width:1.5}.shelf-label{font-family:monospace;font-size:10px;fill:currentColor;text-anchor:middle}.shelf-id{font-family:monospace;font-size:11px;fill:currentColor;text-anchor:middle;font-weight:bold}.shelf-row-label{font-family:monospace;font-size:9px;fill:currentColor;opacity:.5;text-anchor:middle}.shelf-svg a:hover .shelf-box{stroke-width:2.5}</style>
" s)
      (loop for row from 1 to rows do
        (format s "<text x=\"~A\" y=\"~A\" class=\"shelf-row-label\">~A</text>~%"
                (floor lh 2) (+ lh (* (1- row) (+ ch pad)) pad (floor ch 2) 4) row)
        (loop for col from 1 to cols
              for x = (+ lh (* (1- col) (+ cw pad)) pad)
              for y = (+ lh (* (1- row) (+ ch pad)) pad)
              for box = (gethash (cons row col) grid)
              do (cond
                   ((gethash (cons row col) covered) nil)
                   (box
                    (let* ((span (or (parse-integer (or (object-property box :shelf-span) "1") :junk-allowed t) 1))
                           (bw (+ (* span cw) (* (1- span) pad)))
                           (title (truncate-for-svg (object-title box) (+ 14 (* (1- span) 10)))))
                      (format s "<g id=\"~A\" class=\"shelf-box-group\">~%<a href=\"~A\">~%  <rect x=\"~A\" y=\"~A\" width=\"~A\" height=\"~A\" rx=\"3\" class=\"shelf-box\"/>~%  <text x=\"~A\" y=\"~A\" class=\"shelf-id\">~A</text>~%  <text x=\"~A\" y=\"~A\" class=\"shelf-label\">~A</text>~%</a>~%</g>~%"
                              (object-id box) (object-url box) x y bw ch
                              (+ x (floor bw 2)) (+ y 14) (svg-escape title)
                              (+ x (floor bw 2)) (+ y 28)
                              (svg-escape (truncate-for-svg (or (object-property box :contains) "")
                                                            (+ 18 (* (1- span) 12)))))))
                   (t (format s "<rect x=\"~A\" y=\"~A\" width=\"~A\" height=\"~A\" rx=\"3\" class=\"shelf-cell\"/>~%"
                              x y cw ch)))))
      (write-string "</svg>
<script>
document.addEventListener('DOMContentLoaded',function(){
  var h=location.hash.replace('#','');
  if(!h)return;
  document.querySelectorAll('.shelf-box-group').forEach(function(g){
    if(g.getAttribute('id')===h){
      var r=g.querySelector('.shelf-box');
      if(r){r.setAttribute('style','fill:#ffdd00;fill-opacity:0.5;stroke:#333;stroke-width:2')}
      g.querySelectorAll('text').forEach(function(t){t.setAttribute('style','fill:#000')});
      g.scrollIntoView({block:'center'})
    }
  });
});
</script>
" s)
      (dolist (w (nreverse warnings))
        (format s "<div class=\"eval-error\">⚠ ~A</div>~%" w)))))
