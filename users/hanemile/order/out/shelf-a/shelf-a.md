---
title: Shelf A
type: shelf
visibility: public
location: office
cols: 4
rows: 10
---

# Shelf A

(shelf-view "shelf-a" :cols 4 :rows 10)

## Contents

(children "shelf-a" :via "shelf" :show (:title :shelf-row :shelf-col :contains))
