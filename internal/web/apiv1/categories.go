package apiv1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type categoryItem struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// Categories lists the user's active, non-income categories for the transaction
// category picker. The system Income category is excluded (it's not something a
// spend/income transaction is filed under here).
func (a *API) Categories(c *gin.Context) {
	ctx := c.Request.Context()
	uid := c.GetInt64(contextUserID)

	cats, err := a.store.ListCategories(ctx, uid, false)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal", "Could not load categories.")
		return
	}
	groups, _ := a.store.ListGroups(ctx, uid)
	groupName := make(map[int64]string, len(groups))
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}

	out := make([]categoryItem, 0, len(cats))
	for _, cat := range cats {
		if cat.IsIncome {
			continue
		}
		out = append(out, categoryItem{ID: cat.ID, Name: cat.Name, Group: groupName[cat.GroupID]})
	}
	c.JSON(http.StatusOK, gin.H{"categories": out})
}
