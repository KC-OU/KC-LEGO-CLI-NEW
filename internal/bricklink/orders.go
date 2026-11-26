package bricklink

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// ordersTTL is deliberately short: an order list that is minutes old is a
// missed sale, unlike the day-long price/catalogue cache lifetimes above.
const ordersTTL = 30 * time.Second

// SellerOrder is one order received in your store (direction=in).
type SellerOrder struct {
	OrderID     int       `json:"order_id"`
	DateOrdered string    `json:"date_ordered"`
	BuyerName   string    `json:"buyer_name"`
	Status      string    `json:"status"`
	TotalCount  int       `json:"total_count"`
	UniqueCount int       `json:"unique_count"`
	Cost        OrderCost `json:"cost"`
}

type OrderCost struct {
	CurrencyCode string    `json:"currency_code"`
	Subtotal     flexFloat `json:"subtotal"`
	GrandTotal   flexFloat `json:"grand_total"`
}

// SellerOrders lists orders received, filtered to statuses (e.g. "paid,processing,packed"); "" means open ones.
func (c *Client) SellerOrders(ctx context.Context, statuses string) ([]SellerOrder, error) {
	if statuses == "" {
		statuses = "pending,updated,processing,ready,paid,packed"
	}
	q := url.Values{"direction": {"in"}, "status": {statuses}}
	var out []SellerOrder
	if err := c.get(ctx, "/orders", q, ordersTTL, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// OrderItem is one line of an order.
type OrderItem struct {
	InventoryID int `json:"inventory_id"`
	Item        struct {
		No   string `json:"no"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"item"`
	ColorID   int       `json:"color_id"`
	ColorName string    `json:"color_name"`
	Quantity  int       `json:"quantity"`
	NewOrUsed string    `json:"new_or_used"`
	UnitPrice flexFloat `json:"unit_price"`
	Remarks   string    `json:"remarks"`
}

// OrderItems returns an order's lines. BrickLink groups them in batches; they are flattened here.
func (c *Client) OrderItems(ctx context.Context, orderID int) ([]OrderItem, error) {
	var batches [][]OrderItem
	if err := c.get(ctx, "/orders/"+strconv.Itoa(orderID)+"/items", nil, ordersTTL, &batches); err != nil {
		return nil, err
	}
	var out []OrderItem
	for _, b := range batches {
		out = append(out, b...)
	}
	return out, nil
}

// StoreLot is one lot in your store's inventory.
type StoreLot struct {
	InventoryID int `json:"inventory_id"`
	Item        struct {
		No   string `json:"no"`
		Type string `json:"type"`
	} `json:"item"`
	ColorID     int       `json:"color_id"`
	Quantity    int       `json:"quantity"`
	NewOrUsed   string    `json:"new_or_used"`
	UnitPrice   flexFloat `json:"unit_price"`
	Description string    `json:"description"`
}

// StoreInventory lists the lots you have for sale (open ones only).
func (c *Client) StoreInventory(ctx context.Context) ([]StoreLot, error) {
	var out []StoreLot
	if err := c.get(ctx, "/inventories", url.Values{"status": {"Y"}}, ordersTTL, &out); err != nil {
		return nil, err
	}
	return out, nil
}
