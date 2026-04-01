// Collection handlers.
package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"makhor/pkg/db"
	"makhor/pkg/middleware"
	"makhor/pkg/models"
)

// CollectionsPage shows all collections for the current user.
func (h *Handler) CollectionsPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	collections, err := h.DB.GetUserCollections(user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load collections")
		return
	}

	h.render(w, r, "collections.html", map[string]interface{}{
		"Collections": collections,
	})
}

// CreateCollectionPage shows the create collection form.
func (h *Handler) CreateCollectionPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get all tags for selection
	tags, err := h.DB.GetAllTags()
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load tags")
		return
	}

	h.render(w, r, "collection_form.html", map[string]interface{}{
		"Tags":           tags,
		"IsCreate":       true,
		"SelectedTagIDs": make(map[int64]bool),
	})
}

// CreateCollectionSubmit handles collection creation.
func (h *Handler) CreateCollectionSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	tagIDs := r.Form["tags"]

	if name == "" {
		h.renderCollectionFormError(w, r, "Name is required", name, description, nil, true)
		return
	}

	col, err := h.DB.CreateCollection(user.ID, name, description)
	if err != nil {
		h.renderCollectionFormError(w, r, "Failed to create collection: name may already exist", name, description, nil, true)
		return
	}

	// Add selected tags
	for _, tagIDStr := range tagIDs {
		tagID, err := strconv.ParseInt(tagIDStr, 10, 64)
		if err == nil {
			h.DB.AddTagToCollection(col.ID, tagID)
		}
	}

	http.Redirect(w, r, "/collections/"+strconv.FormatInt(col.ID, 10), http.StatusSeeOther)
}

// CollectionDetailPage shows a single collection with its posts.
func (h *Handler) CollectionDetailPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract collection ID from path: /collections/123
	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	path = strings.TrimSuffix(path, "/edit")
	path = strings.TrimSuffix(path, "/delete")
	path = strings.TrimSuffix(path, "/add-tag")
	path = strings.TrimSuffix(path, "/remove-tag")

	colID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid collection ID")
		return
	}

	col, err := h.DB.GetCollectionByID(colID, user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Collection not found")
		return
	}

	// Get pagination
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	perPage := DefaultPostsPerPage
	posts, total, err := h.DB.GetPostsForCollection(colID, page, perPage, &user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load posts")
		return
	}

	pagination := models.NewPagination(page, perPage, total)

	h.render(w, r, "collection_detail.html", map[string]interface{}{
		"Collection": col,
		"Posts":      posts,
		"Total":      total,
		"Pagination": pagination,
		"Page":       page,
		"TotalPages": pagination.TotalPages,
		"HasPrev":    pagination.HasPrev(),
		"HasNext":    pagination.HasNext(),
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
	})
}

// EditCollectionPage shows the edit collection form.
func (h *Handler) EditCollectionPage(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract collection ID
	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	path = strings.TrimSuffix(path, "/edit")
	colID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid collection ID")
		return
	}

	col, err := h.DB.GetCollectionByID(colID, user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Collection not found")
		return
	}

	// Get all tags for selection
	tags, err := h.DB.GetAllTags()
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to load tags")
		return
	}

	// Mark which tags are in collection
	tagIDMap := make(map[int64]bool)
	for _, t := range col.Tags {
		tagIDMap[t.ID] = true
	}

	h.render(w, r, "collection_form.html", map[string]interface{}{
		"Collection":     col,
		"Tags":           tags,
		"SelectedTagIDs": tagIDMap,
		"IsCreate":       false,
	})
}

// EditCollectionSubmit handles collection updates.
func (h *Handler) EditCollectionSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract collection ID
	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	path = strings.TrimSuffix(path, "/edit")
	colID, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid collection ID")
		return
	}

	col, err := h.DB.GetCollectionByID(colID, user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusNotFound, "Collection not found")
		return
	}

	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	tagIDs := r.Form["tags"]

	if name == "" {
		h.renderCollectionFormError(w, r, "Name is required", name, description, col, false)
		return
	}

	err = h.DB.UpdateCollection(colID, user.ID, name, description)
	if err != nil {
		h.renderCollectionFormError(w, r, "Failed to update collection", name, description, col, false)
		return
	}

	// Update tags: remove all then add selected
	for _, t := range col.Tags {
		h.DB.RemoveTagFromCollection(colID, t.ID)
	}
	for _, tagIDStr := range tagIDs {
		tagID, err := strconv.ParseInt(tagIDStr, 10, 64)
		if err == nil {
			h.DB.AddTagToCollection(colID, tagID)
		}
	}

	http.Redirect(w, r, "/collections/"+strconv.FormatInt(colID, 10), http.StatusSeeOther)
}

// DeleteCollectionSubmit handles collection deletion.
func (h *Handler) DeleteCollectionSubmit(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	colIDStr := r.URL.Query().Get("id")
	colID, err := strconv.ParseInt(colIDStr, 10, 64)
	if err != nil {
		h.renderError(w, r, http.StatusBadRequest, "Invalid collection ID")
		return
	}

	err = h.DB.DeleteCollection(colID, user.ID)
	if err != nil {
		h.renderError(w, r, http.StatusInternalServerError, "Failed to delete collection")
		return
	}

	http.Redirect(w, r, "/collections", http.StatusSeeOther)
}

// renderCollectionFormError renders the collection form with an error message.
func (h *Handler) renderCollectionFormError(w http.ResponseWriter, r *http.Request, errMsg, name, description string, col *db.Collection, isCreate bool) {
	tags, _ := h.DB.GetAllTags()

	tagIDMap := make(map[int64]bool)
	if col != nil {
		for _, t := range col.Tags {
			tagIDMap[t.ID] = true
		}
	}

	h.render(w, r, "collection_form.html", map[string]interface{}{
		"Error":          errMsg,
		"Name":           name,
		"Description":    description,
		"Collection":     col,
		"Tags":           tags,
		"SelectedTagIDs": tagIDMap,
		"IsCreate":       isCreate,
	})
}
