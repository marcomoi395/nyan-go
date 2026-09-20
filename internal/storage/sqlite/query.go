package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nyan-go/internal/ledger"
)

type SearchFilter struct {
	UserID         string
	GuildID        string
	ChannelID      string
	Start          time.Time
	End            time.Time
	Type           ledger.TransactionType
	Category       ledger.Category
	NoteContains   string
	MinAmountVND   ledger.AmountVND
	MaxAmountVND   ledger.AmountVND
	IncludeDeleted bool
}

type SearchResult struct {
	Transactions []ledger.Transaction
	Total        int
	Truncated    bool
}

func (s *Store) Search(ctx context.Context, filter SearchFilter) (SearchResult, error) {
	where, args, err := transactionWhere(filter.UserID, filter.GuildID, filter.ChannelID, filter.Start, filter.End, filter.Type, filter.Category, filter.NoteContains, filter.MinAmountVND, filter.MaxAmountVND, filter.IncludeDeleted)
	if err != nil {
		return SearchResult{}, err
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM transactions WHERE `+where, args...).Scan(&total); err != nil {
		return SearchResult{}, fmt.Errorf("count transactions: %w", err)
	}
	queryArgs := append(append([]any{}, args...), maxSearchRows)
	rows, err := s.db.QueryContext(ctx, `SELECT `+transactionColumns+` FROM transactions WHERE `+where+` ORDER BY occurred_at DESC, id DESC LIMIT ?`, queryArgs...)
	if err != nil {
		return SearchResult{}, fmt.Errorf("search transactions: %w", err)
	}
	defer rows.Close()
	transactions, err := scanTransactions(rows)
	if err != nil {
		return SearchResult{}, err
	}
	return SearchResult{Transactions: transactions, Total: total, Truncated: total > len(transactions)}, nil
}

type StatisticsFilter struct {
	UserID       string
	GuildID      string
	ChannelID    string
	Start        time.Time
	End          time.Time
	Type         ledger.TransactionType
	Category     ledger.Category
	Group        string
	CompareStart time.Time
	CompareEnd   time.Time
}

type Statistics struct {
	Currency         string
	Timezone         string
	Start            time.Time
	End              time.Time
	Type             ledger.TransactionType
	Category         ledger.Category
	Group            string
	TransactionCount int
	TotalIncome      ledger.AmountVND
	TotalExpense     ledger.AmountVND
	Net              ledger.AmountVND
	Groups           []StatisticsGroup
	Comparison       *StatisticsComparison
}

type StatisticsGroup struct {
	Key              string
	TransactionCount int
	TotalIncome      ledger.AmountVND
	TotalExpense     ledger.AmountVND
	Net              ledger.AmountVND
}

type StatisticsComparison struct {
	Start            time.Time
	End              time.Time
	TransactionCount int
	TotalIncome      ledger.AmountVND
	TotalExpense     ledger.AmountVND
	Net              ledger.AmountVND
	NetDelta         ledger.AmountVND
	NetPercentage    float64
}

func (s *Store) Statistics(ctx context.Context, filter StatisticsFilter) (Statistics, error) {
	if strings.TrimSpace(filter.UserID) == "" || strings.TrimSpace(filter.GuildID) == "" || strings.TrimSpace(filter.ChannelID) == "" {
		return Statistics{}, errors.New("statistics identity fields are required")
	}
	if filter.Start.IsZero() || filter.End.IsZero() || !filter.Start.Before(filter.End) {
		return Statistics{}, errors.New("statistics requires a valid date range")
	}
	group := filter.Group
	if group == "" {
		group = "total"
	}
	if group != "total" && group != "day" && group != "category" && group != "type" {
		return Statistics{}, fmt.Errorf("unsupported statistics grouping %q", group)
	}
	where, args, err := transactionWhere(filter.UserID, filter.GuildID, filter.ChannelID, filter.Start, filter.End, filter.Type, filter.Category, "", 0, 0, false)
	if err != nil {
		return Statistics{}, err
	}
	result, err := s.statisticsQuery(ctx, where, args, group)
	if err != nil {
		return Statistics{}, err
	}
	result.Currency = "VND"
	result.Timezone = "Asia/Ho_Chi_Minh"
	result.Start, result.End, result.Type, result.Category, result.Group = filter.Start, filter.End, filter.Type, filter.Category, group
	if !filter.CompareStart.IsZero() || !filter.CompareEnd.IsZero() {
		if filter.CompareStart.IsZero() || filter.CompareEnd.IsZero() || !filter.CompareStart.Before(filter.CompareEnd) {
			return Statistics{}, errors.New("comparison requires a valid date range")
		}
		compareWhere, compareArgs, err := transactionWhere(filter.UserID, filter.GuildID, filter.ChannelID, filter.CompareStart, filter.CompareEnd, filter.Type, filter.Category, "", 0, 0, false)
		if err != nil {
			return Statistics{}, err
		}
		comparison, err := s.statisticsQuery(ctx, compareWhere, compareArgs, "total")
		if err != nil {
			return Statistics{}, err
		}
		comparison.Start, comparison.End = filter.CompareStart, filter.CompareEnd
		netDelta := result.Net - comparison.Net
		netPercentage := 0.0
		if comparison.Net != 0 {
			netPercentage = float64(netDelta) / float64(comparison.Net) * 100
		}
		result.Comparison = &StatisticsComparison{Start: comparison.Start, End: comparison.End, TransactionCount: comparison.TransactionCount, TotalIncome: comparison.TotalIncome, TotalExpense: comparison.TotalExpense, Net: comparison.Net, NetDelta: netDelta, NetPercentage: netPercentage}
	}
	return result, nil
}

func (s *Store) statisticsQuery(ctx context.Context, where string, args []any, group string) (Statistics, error) {
	var result Statistics
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN type = 'income' THEN amount_vnd ELSE 0 END), 0), COALESCE(SUM(CASE WHEN type = 'expense' THEN amount_vnd ELSE 0 END), 0) FROM transactions WHERE `+where, args...).Scan(&result.TransactionCount, &result.TotalIncome, &result.TotalExpense); err != nil {
		return Statistics{}, fmt.Errorf("calculate statistics: %w", err)
	}
	result.Net = result.TotalIncome - result.TotalExpense
	if group == "total" {
		return result, nil
	}
	groupExpr := map[string]string{"day": `substr(occurred_at, 1, 10)`, "category": "category", "type": "type"}[group]
	query := `SELECT ` + groupExpr + `, COUNT(*), COALESCE(SUM(CASE WHEN type = 'income' THEN amount_vnd ELSE 0 END), 0), COALESCE(SUM(CASE WHEN type = 'expense' THEN amount_vnd ELSE 0 END), 0) FROM transactions WHERE ` + where + ` GROUP BY ` + groupExpr + ` ORDER BY ` + groupExpr
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Statistics{}, fmt.Errorf("group statistics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var groupResult StatisticsGroup
		if err := rows.Scan(&groupResult.Key, &groupResult.TransactionCount, &groupResult.TotalIncome, &groupResult.TotalExpense); err != nil {
			return Statistics{}, fmt.Errorf("scan grouped statistics: %w", err)
		}
		groupResult.Net = groupResult.TotalIncome - groupResult.TotalExpense
		result.Groups = append(result.Groups, groupResult)
	}
	if err := rows.Err(); err != nil {
		return Statistics{}, fmt.Errorf("read grouped statistics: %w", err)
	}
	return result, nil
}

func transactionWhere(userID, guildID, channelID string, start, end time.Time, typ ledger.TransactionType, category ledger.Category, note string, minAmount, maxAmount ledger.AmountVND, includeDeleted bool) (string, []any, error) {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(guildID) == "" || strings.TrimSpace(channelID) == "" {
		return "", nil, errors.New("transaction identity fields are required")
	}
	where := []string{"creator_user_id = ?", "guild_id = ?", "channel_id = ?"}
	args := []any{userID, guildID, channelID}
	if !start.IsZero() || !end.IsZero() {
		if start.IsZero() || end.IsZero() || !start.Before(end) {
			return "", nil, errors.New("invalid transaction date range")
		}
		where = append(where, "occurred_at >= ?", "occurred_at < ?")
		args = append(args, formatTime(start), formatTime(end))
	}
	if typ != "" {
		if err := ledger.ValidateTransactionType(typ); err != nil {
			return "", nil, err
		}
		where, args = append(where, "type = ?"), append(args, typ)
	}
	if category != "" {
		where, args = append(where, "category = ?"), append(args, category)
	}
	if note != "" {
		where, args = append(where, "instr(note, ?) > 0"), append(args, note)
	}
	if minAmount < 0 || maxAmount < 0 || (maxAmount > 0 && minAmount > maxAmount) {
		return "", nil, errors.New("invalid amount range")
	}
	if minAmount > 0 {
		where, args = append(where, "amount_vnd >= ?"), append(args, minAmount)
	}
	if maxAmount > 0 {
		where, args = append(where, "amount_vnd <= ?"), append(args, maxAmount)
	}
	if !includeDeleted {
		where = append(where, "deleted_at IS NULL")
	}
	return strings.Join(where, " AND "), args, nil
}

const transactionColumns = `id, type, amount_vnd, category, note, occurred_at, creator_user_id, guild_id, channel_id, source_message_id, created_at, updated_at, deleted_at`

func getTransaction(ctx context.Context, tx *sql.Tx, id int64, includeDeleted bool) (ledger.Transaction, error) {
	where := "id = ?"
	if !includeDeleted {
		where += " AND deleted_at IS NULL"
	}
	row := tx.QueryRowContext(ctx, `SELECT `+transactionColumns+` FROM transactions WHERE `+where, id)
	return scanTransaction(row)
}

func getTransactionsByIDs(ctx context.Context, tx *sql.Tx, ids []int64, includeDeleted bool) ([]ledger.Transaction, error) {
	result := make([]ledger.Transaction, 0, len(ids))
	for _, id := range ids {
		transaction, err := getTransaction(ctx, tx, id, includeDeleted)
		if err != nil {
			return nil, fmt.Errorf("read transaction %d: %w", id, err)
		}
		result = append(result, transaction)
	}
	return result, nil
}

type scanner interface{ Scan(...any) error }

func scanTransaction(row scanner) (ledger.Transaction, error) {
	var result ledger.Transaction
	var typ, category, occurredAt, createdAt, updatedAt string
	var deletedAt sql.NullString
	if err := row.Scan(&result.ID, &typ, &result.AmountVND, &category, &result.Note, &occurredAt, &result.CreatorUserID, &result.GuildID, &result.ChannelID, &result.SourceMessageID, &createdAt, &updatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, fmt.Errorf("transaction not found: %w", err)
		}
		return result, fmt.Errorf("scan transaction: %w", err)
	}
	result.Type, result.Category = ledger.TransactionType(typ), ledger.Category(category)
	var err error
	if result.OccurredAt, err = parseTime(occurredAt); err != nil {
		return result, err
	}
	if result.CreatedAt, err = parseTime(createdAt); err != nil {
		return result, err
	}
	if result.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return result, err
	}
	if deletedAt.Valid {
		value, err := parseTime(deletedAt.String)
		if err != nil {
			return result, err
		}
		result.DeletedAt = &value
	}
	return result, nil
}

func scanTransactions(rows *sql.Rows) ([]ledger.Transaction, error) {
	var result []ledger.Transaction
	for rows.Next() {
		transaction, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, transaction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read transactions: %w", err)
	}
	return result, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseTime(value string) (time.Time, error) {
	result, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlite timestamp %q: %w", value, err)
	}
	return result, nil
}

func insertAudit(ctx context.Context, tx *sql.Tx, request ledger.RequestContext, transactionID int64, action string, before *ledger.Transaction, after any) error {
	var beforeJSON, afterJSON any
	if before != nil {
		encoded, err := json.Marshal(before)
		if err != nil {
			return fmt.Errorf("encode audit before state: %w", err)
		}
		beforeJSON = string(encoded)
	}
	if after != nil {
		encoded, err := json.Marshal(after)
		if err != nil {
			return fmt.Errorf("encode audit after state: %w", err)
		}
		afterJSON = string(encoded)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(transaction_id, user_id, guild_id, channel_id, source_message_id, action, occurred_at, before_json, after_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, transactionID, request.UserID, request.GuildID, request.ChannelID, request.SourceMessageID, action, formatTime(request.ReceivedAt), beforeJSON, afterJSON); err != nil {
		return fmt.Errorf("record audit event: %w", err)
	}
	return nil
}
