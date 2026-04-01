// makhor-admin is a CLI tool for administrative operations.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"makhor/pkg/db"
)

func main() {
	dbPath := flag.String("db", "makhor.db", "SQLite database path")
	flag.Parse()

	if flag.NArg() < 1 {
		printUsage()
		os.Exit(1)
	}

	database, err := db.New(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	cmd := flag.Arg(0)
	args := flag.Args()[1:]

	switch cmd {
	case "promote":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin promote <username>")
			os.Exit(1)
		}
		promoteUser(database, args[0])

	case "demote":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin demote <username>")
			os.Exit(1)
		}
		demoteUser(database, args[0])

	case "ban":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin ban <username>")
			os.Exit(1)
		}
		banUser(database, args[0])

	case "unban":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin unban <username>")
			os.Exit(1)
		}
		unbanUser(database, args[0])

	case "list-users":
		listUsers(database)

	case "list-admins":
		listAdmins(database)

	case "user-info":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin user-info <username>")
			os.Exit(1)
		}
		userInfo(database, args[0])

	case "delete-post":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin delete-post <post_id>")
			os.Exit(1)
		}
		deletePost(database, args[0])

	case "delete-comment":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin delete-comment <comment_id>")
			os.Exit(1)
		}
		deleteComment(database, args[0])

	case "delete-tag":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin delete-tag <tag_name>")
			os.Exit(1)
		}
		deleteTag(database, args[0])

	case "list-tags":
		listTags(database)

	case "tag-admin-add":
		if len(args) < 2 {
			fmt.Println("Usage: makhor-admin tag-admin-add <tag_name> <username>")
			os.Exit(1)
		}
		addTagAdmin(database, args[0], args[1])

	case "tag-admin-remove":
		if len(args) < 2 {
			fmt.Println("Usage: makhor-admin tag-admin-remove <tag_name> <username>")
			os.Exit(1)
		}
		removeTagAdmin(database, args[0], args[1])

	case "seed-tag-creators":
		seedTagCreators(database)

	case "feed-add":
		if len(args) < 2 {
			fmt.Println("Usage: makhor-admin feed-add <url> <tag_name> [interval_minutes]")
			os.Exit(1)
		}
		interval := 60
		if len(args) >= 3 {
			fmt.Sscanf(args[2], "%d", &interval)
		}
		addFeed(database, args[0], args[1], interval)

	case "feed-list":
		listFeeds(database)

	case "feed-delete":
		if len(args) < 1 {
			fmt.Println("Usage: makhor-admin feed-delete <feed_id>")
			os.Exit(1)
		}
		deleteFeed(database, args[0])

	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`makhor-admin - Administrative CLI for makhor

Usage: makhor-admin [options] <command> [args...]

Options:
  -db string    SQLite database path (default "makhor.db")

Commands:
  User Management:
    promote <username>       Make user a site admin
    demote <username>        Remove admin status from user
    ban <username>           Ban a user
    unban <username>         Unban a user
    list-users               List all users
    list-admins              List all admin users
    user-info <username>     Show detailed user info

  Content Moderation:
    delete-post <id>         Delete a post
    delete-comment <id>      Delete a comment

  Tag Management:
    list-tags                      List all tags with creators
    delete-tag <name>              Deactivate a tag
    tag-admin-add <tag> <user>     Add user as tag admin
    tag-admin-remove <tag> <user>  Remove user as tag admin
    seed-tag-creators              Set first admin as creator for tags without one

  RSS Feed Management:
    feed-add <url> <tag> [mins]    Add RSS feed to poll (default 60 min interval)
    feed-list                      List all RSS feeds
    feed-delete <id>               Delete an RSS feed

Examples:
    makhor-admin promote alice
    makhor-admin -db /path/to/makhor.db list-admins
    makhor-admin tag-admin-add programming bob`)
}

func promoteUser(d *db.DB, username string) {
	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	_, err = d.Exec("UPDATE users SET is_admin = TRUE WHERE id = ?", user.ID)
	if err != nil {
		log.Fatalf("Failed to promote user: %v", err)
	}

	fmt.Printf("User '%s' is now a site admin.\n", username)
}

func demoteUser(d *db.DB, username string) {
	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	_, err = d.Exec("UPDATE users SET is_admin = FALSE WHERE id = ?", user.ID)
	if err != nil {
		log.Fatalf("Failed to demote user: %v", err)
	}

	fmt.Printf("User '%s' is no longer a site admin.\n", username)
}

func banUser(d *db.DB, username string) {
	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	_, err = d.Exec("UPDATE users SET is_banned = TRUE WHERE id = ?", user.ID)
	if err != nil {
		log.Fatalf("Failed to ban user: %v", err)
	}

	fmt.Printf("User '%s' has been banned.\n", username)
}

func unbanUser(d *db.DB, username string) {
	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	_, err = d.Exec("UPDATE users SET is_banned = FALSE WHERE id = ?", user.ID)
	if err != nil {
		log.Fatalf("Failed to unban user: %v", err)
	}

	fmt.Printf("User '%s' has been unbanned.\n", username)
}

func listUsers(d *db.DB) {
	rows, err := d.Query(`
		SELECT id, username, email, is_admin, is_banned, created_at
		FROM users ORDER BY id
	`)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}
	defer rows.Close()

	fmt.Printf("%-5s %-20s %-30s %-8s %-8s %s\n", "ID", "Username", "Email", "Admin", "Banned", "Created")
	fmt.Println("--------------------------------------------------------------------------------------------")

	for rows.Next() {
		var id int64
		var username, email, createdAt string
		var isAdmin, isBanned bool
		if err := rows.Scan(&id, &username, &email, &isAdmin, &isBanned, &createdAt); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		adminStr := ""
		if isAdmin {
			adminStr = "YES"
		}
		bannedStr := ""
		if isBanned {
			bannedStr = "YES"
		}
		fmt.Printf("%-5d %-20s %-30s %-8s %-8s %s\n", id, username, email, adminStr, bannedStr, createdAt[:10])
	}
}

func listAdmins(d *db.DB) {
	rows, err := d.Query(`
		SELECT id, username, email, created_at
		FROM users WHERE is_admin = TRUE ORDER BY id
	`)
	if err != nil {
		log.Fatalf("Failed to query admins: %v", err)
	}
	defer rows.Close()

	fmt.Printf("%-5s %-20s %-30s %s\n", "ID", "Username", "Email", "Created")
	fmt.Println("----------------------------------------------------------------------")

	count := 0
	for rows.Next() {
		var id int64
		var username, email, createdAt string
		if err := rows.Scan(&id, &username, &email, &createdAt); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		fmt.Printf("%-5d %-20s %-30s %s\n", id, username, email, createdAt[:10])
		count++
	}

	if count == 0 {
		fmt.Println("No admin users found.")
	}
}

func userInfo(d *db.DB, username string) {
	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	fmt.Printf("User: %s\n", user.Username)
	fmt.Printf("  ID:       %d\n", user.ID)
	fmt.Printf("  Email:    %s\n", user.Email)
	fmt.Printf("  Admin:    %v\n", user.IsAdmin)
	fmt.Printf("  Banned:   %v\n", user.IsBanned)
	fmt.Printf("  Created:  %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
	if user.InvitedBy != nil {
		inviter, _ := d.GetUserByID(*user.InvitedBy)
		if inviter != nil {
			fmt.Printf("  Invited by: %s\n", inviter.Username)
		}
	}

	// Count posts and comments
	var postCount, commentCount int
	d.QueryRow("SELECT COUNT(*) FROM posts WHERE user_id = ? AND is_deleted = FALSE", user.ID).Scan(&postCount)
	d.QueryRow("SELECT COUNT(*) FROM comments WHERE user_id = ? AND is_deleted = FALSE", user.ID).Scan(&commentCount)
	fmt.Printf("  Posts:    %d\n", postCount)
	fmt.Printf("  Comments: %d\n", commentCount)

	// List tag admin roles
	rows, _ := d.Query(`
		SELECT t.name FROM tag_admins ta
		JOIN tags t ON ta.tag_id = t.id
		WHERE ta.user_id = ?
	`, user.ID)
	if rows != nil {
		defer rows.Close()
		var tagRoles []string
		for rows.Next() {
			var tagName string
			rows.Scan(&tagName)
			tagRoles = append(tagRoles, tagName)
		}
		if len(tagRoles) > 0 {
			fmt.Printf("  Tag admin: %v\n", tagRoles)
		}
	}

	// List tags created
	rows2, _ := d.Query("SELECT name FROM tags WHERE creator_id = ?", user.ID)
	if rows2 != nil {
		defer rows2.Close()
		var createdTags []string
		for rows2.Next() {
			var tagName string
			rows2.Scan(&tagName)
			createdTags = append(createdTags, tagName)
		}
		if len(createdTags) > 0 {
			fmt.Printf("  Tags created: %v\n", createdTags)
		}
	}
}

func deletePost(d *db.DB, postID string) {
	_, err := d.Exec("UPDATE posts SET is_deleted = TRUE WHERE id = ?", postID)
	if err != nil {
		log.Fatalf("Failed to delete post: %v", err)
	}
	fmt.Printf("Post %s has been deleted.\n", postID)
}

func deleteComment(d *db.DB, commentID string) {
	_, err := d.Exec("UPDATE comments SET is_deleted = TRUE WHERE id = ?", commentID)
	if err != nil {
		log.Fatalf("Failed to delete comment: %v", err)
	}
	fmt.Printf("Comment %s has been deleted.\n", commentID)
}

func deleteTag(d *db.DB, tagName string) {
	result, err := d.Exec("UPDATE tags SET is_active = FALSE WHERE name = ?", tagName)
	if err != nil {
		log.Fatalf("Failed to delete tag: %v", err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		fmt.Printf("Tag '%s' not found.\n", tagName)
	} else {
		fmt.Printf("Tag '%s' has been deactivated.\n", tagName)
	}
}

func listTags(d *db.DB) {
	rows, err := d.Query(`
		SELECT t.id, t.name, t.is_active, COALESCE(u.username, '-') as creator,
		       (SELECT COUNT(*) FROM post_tags WHERE tag_id = t.id) as post_count
		FROM tags t
		LEFT JOIN users u ON t.creator_id = u.id
		ORDER BY t.name
	`)
	if err != nil {
		log.Fatalf("Failed to query tags: %v", err)
	}
	defer rows.Close()

	fmt.Printf("%-5s %-40s %-8s %-15s %s\n", "ID", "Name", "Active", "Creator", "Posts")
	fmt.Println("-------------------------------------------------------------------------------")

	for rows.Next() {
		var id int64
		var name, creator string
		var isActive bool
		var postCount int
		if err := rows.Scan(&id, &name, &isActive, &creator, &postCount); err != nil {
			log.Printf("Error scanning row: %v", err)
			continue
		}
		activeStr := "YES"
		if !isActive {
			activeStr = "no"
		}
		fmt.Printf("%-5d %-40s %-8s %-15s %d\n", id, name, activeStr, creator, postCount)
	}
}

func addTagAdmin(d *db.DB, tagName, username string) {
	tag, err := d.GetTagByName(tagName)
	if err != nil {
		log.Fatalf("Tag not found: %v", err)
	}

	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	err = d.AddTagAdmin(tag.ID, user.ID, user.ID)
	if err != nil {
		log.Fatalf("Failed to add tag admin: %v", err)
	}

	fmt.Printf("User '%s' is now an admin of tag '%s'.\n", username, tagName)
}

func removeTagAdmin(d *db.DB, tagName, username string) {
	tag, err := d.GetTagByName(tagName)
	if err != nil {
		log.Fatalf("Tag not found: %v", err)
	}

	user, err := d.GetUserByUsername(username)
	if err != nil {
		log.Fatalf("User not found: %v", err)
	}

	err = d.RemoveTagAdmin(tag.ID, user.ID)
	if err != nil {
		log.Fatalf("Failed to remove tag admin: %v", err)
	}

	fmt.Printf("User '%s' is no longer an admin of tag '%s'.\n", username, tagName)
}

func seedTagCreators(d *db.DB) {
	err := d.SeedTagCreators()
	if err != nil {
		log.Fatalf("Failed to seed tag creators: %v", err)
	}
	fmt.Println("Tag creators have been seeded with the first admin user.")
}

func addFeed(d *db.DB, url, tagName string, intervalMinutes int) {
	tag, err := d.GetTagByName(tagName)
	if err != nil {
		log.Fatalf("Tag not found: %v", err)
	}

	feed, err := d.CreateRSSFeed(url, "", tag.ID, intervalMinutes)
	if err != nil {
		log.Fatalf("Failed to create feed: %v", err)
	}

	fmt.Printf("Created RSS feed %d for tag '%s' (polling every %d minutes)\n", feed.ID, tagName, intervalMinutes)
	fmt.Printf("URL: %s\n", url)
}

func listFeeds(d *db.DB) {
	feeds, err := d.GetAllRSSFeeds()
	if err != nil {
		log.Fatalf("Failed to list feeds: %v", err)
	}

	if len(feeds) == 0 {
		fmt.Println("No RSS feeds configured.")
		return
	}

	fmt.Printf("%-5s %-50s %-20s %-8s %-8s %s\n", "ID", "URL", "Tag", "Interval", "Active", "Last Polled")
	fmt.Println("--------------------------------------------------------------------------------------------------------------")

	for _, feed := range feeds {
		lastPolled := "-"
		if feed.LastPolled != nil {
			lastPolled = feed.LastPolled.Format("2006-01-02 15:04")
		}
		activeStr := "YES"
		if !feed.IsActive {
			activeStr = "no"
		}

		urlDisplay := feed.URL
		if len(urlDisplay) > 48 {
			urlDisplay = urlDisplay[:45] + "..."
		}

		fmt.Printf("%-5d %-50s %-20s %-8d %-8s %s\n",
			feed.ID, urlDisplay, feed.TagName, feed.IntervalMinutes, activeStr, lastPolled)

		if feed.LastError != "" {
			fmt.Printf("      Error: %s\n", feed.LastError)
		}
	}

	fmt.Printf("\nTotal: %d feeds\n", len(feeds))
}

func deleteFeed(d *db.DB, feedID string) {
	var id int64
	_, err := fmt.Sscanf(feedID, "%d", &id)
	if err != nil {
		log.Fatalf("Invalid feed ID: %s", feedID)
	}

	err = d.DeleteRSSFeed(id)
	if err != nil {
		log.Fatalf("Failed to delete feed: %v", err)
	}

	fmt.Printf("Feed %d has been deleted.\n", id)
}
