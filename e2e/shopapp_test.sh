#!/bin/bash
#
# E-commerce Application API - End-to-End Test Suite
#
# Tests the shopapp configuration which exercises:
# - Order state machine (pending -> paid -> shipped -> delivered | cancelled)
# - Product catalog with categories
# - Inventory management with stock tracking
# - Batch operations (bulk price updates)
# - Price calculations and filtering
#
# TODO/WORKAROUNDS (revisit after fixes):
# - [WORKAROUND] State machine validation done in SQL rather than conditions
#   because conditions can't access step data for complex validation.
#   Fix: Add template function support to conditions or array indexing.
# - [WORKAROUND] No auth implemented - would need query param auth like crmapp.
#   Fix: Add params: field to StepConfig for header-based auth.
#
# Usage:
#   ./e2e/shopapp_test.sh           # Run tests without coverage
#   ./e2e/shopapp_test.sh --cover   # Run with coverage

set -euo pipefail

# ============================================================================
# SETUP
# ============================================================================

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Source shared libraries
source "$SCRIPT_DIR/lib/helpers.sh"
source "$SCRIPT_DIR/lib/runner.sh"

# App configuration
APP_NAME="shopapp"

# Parse command line arguments
parse_args "$@"

# Set up test environment
setup_test_env "$APP_NAME"

# Config template (uses PROJECT_ROOT from setup_test_env)
CONFIG_TEMPLATE="$PROJECT_ROOT/testdata/shopapp.yaml"

# Database path substitution
declare -A DB_VARS
DB_VARS[DB_PATH]="$TEMP_DIR/shopapp.db"

# ============================================================================
# TEST CASES
# ============================================================================

test_init_database() {
    header "Database Initialization"

    POST /api/init
    expect_status 201 "Init returns 201"
    expect_json '.success' 'true' "Init succeeds"
    expect_contains "seed data" "Response mentions seed data"
}

# ----------------------------------------------------------------------------
# Category Tests
# ----------------------------------------------------------------------------

test_list_categories() {
    header "List Categories"

    GET /api/categories
    expect_status 200 "List categories returns 200"
    expect_contains "categories" "Response contains categories"

    local count=$(json_val '.count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -ge 10 ]; then
        success "Seed data has categories (count=$count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected at least 10 categories from seed data"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_list_categories_caching() {
    header "List Categories - Caching"

    # Note: Categories may already be cached from previous test
    GET /api/categories
    local first_cache=$(echo "$_response_headers" | grep -i "^X-Cache:" | sed 's/.*: //' | tr -d '\r\n')
    info "First request cache status: $first_cache"

    EXPECT_CACHE_HIT=true
    GET /api/categories
    expect_header "X-Cache" "HIT" "Second request is cache HIT"
    reset_expectations
}

test_get_category() {
    header "Get Single Category"

    # Get Electronics category (id=1) which has subcategories
    GET /api/categories/1
    expect_status 200 "Get category returns 200"
    expect_contains "category" "Response contains category"
    expect_contains "subcategories" "Response contains subcategories"
    expect_json '.category.name' 'Electronics' "Category name is Electronics"
}

test_get_category_with_products() {
    header "Get Category - With Products"

    # Get Computers category (id=2) which has products
    GET /api/categories/2
    expect_status 200 "Get category returns 200"
    expect_contains "products" "Response contains products"

    local product_count=$(json_val '.product_count')
    info "Computers category has $product_count products"
}

test_get_category_not_found() {
    header "Get Category - Not Found"

    GET /api/categories/99999
    expect_status 404 "Missing category returns 404"
    expect_json '.error' 'Category not found' "Error message correct"
}

# ----------------------------------------------------------------------------
# Product Tests
# ----------------------------------------------------------------------------

test_list_products() {
    header "List Products"

    GET /api/products
    expect_status 200 "List products returns 200"
    expect_contains "products" "Response contains products"

    local count=$(json_val '.count')
    local total=$(json_val '.total')
    info "Products returned: $count, total: $total"

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$total" -ge 40 ]; then
        success "Seed data has many products (total=$total)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected at least 40 products from seed data"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_list_products_filtering() {
    header "List Products - Filtering"

    # Filter by category
    GET "/api/products?category_id=2"
    expect_status 200 "Filter by category returns 200"
    local count=$(json_val '.count')
    info "Products in Computers category: $count"

    # Filter by status
    GET "/api/products?status=active"
    expect_status 200 "Filter by status returns 200"

    # Filter by price range
    GET "/api/products?min_price=100&max_price=500"
    expect_status 200 "Filter by price range returns 200"
    local price_count=$(json_val '.count')
    info "Products in price range 100-500: $price_count"

    # Filter by in-stock
    GET "/api/products?in_stock=true"
    expect_status 200 "Filter by in_stock returns 200"
}

test_list_products_pagination() {
    header "List Products - Pagination"

    GET "/api/products?limit=5&offset=0"
    expect_status 200 "First page returns 200"
    expect_json '.limit' '5' "Limit is 5"
    expect_json '.offset' '0' "Offset is 0"

    local first_count=$(json_val '.count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$first_count" -le 5 ]; then
        success "Pagination respects limit (count=$first_count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Pagination should respect limit"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    GET "/api/products?limit=5&offset=5"
    expect_status 200 "Second page returns 200"
    expect_json '.offset' '5' "Offset is 5"
}

test_get_product() {
    header "Get Single Product"

    GET /api/products/1
    expect_status 200 "Get product returns 200"
    expect_contains "product" "Response contains product"
    expect_json '.product.sku' 'LAPTOP-001' "Product SKU is LAPTOP-001"
    expect_contains "category_name" "Product includes category_name"
}

test_get_product_caching() {
    header "Get Product - Caching"

    # Use a product not fetched before
    EXPECT_CACHE_MISS=true
    GET /api/products/5
    expect_header "X-Cache" "MISS" "First request is cache MISS"

    EXPECT_CACHE_MISS=false
    EXPECT_CACHE_HIT=true
    GET /api/products/5
    expect_header "X-Cache" "HIT" "Second request is cache HIT"
    reset_expectations
}

test_get_product_not_found() {
    header "Get Product - Not Found"

    GET /api/products/99999
    expect_status 404 "Missing product returns 404"
    expect_json '.error' 'Product not found' "Error message correct"
}

test_update_price() {
    header "Update Product Price"

    # Use product 20 which hasn't been fetched (to avoid cache issues)
    # Update price
    PUT /api/products/20/price price=175.99
    expect_status 200 "Update price returns 200"
    expect_json '.success' 'true' "Update succeeds"
    expect_json '.new_price' '175.99' "New price is 175.99"

    # Response includes old_price so we can verify the change
    local old_price=$(json_val '.old_price')
    info "Changed from $old_price to 175.99"
}

test_update_price_not_found() {
    header "Update Price - Not Found"

    PUT /api/products/99999/price price=10.00
    expect_status 404 "Missing product returns 404"
}

test_batch_update_prices() {
    header "Batch Update Prices"

    # Apply 10% increase to Audio category (id=4)
    POST /api/products/batch-price category_id=4 adjustment_percent=10
    expect_status 200 "Batch update returns 200"
    expect_json '.success' 'true' "Batch update succeeds"

    local updated=$(json_val '.products_updated')
    info "Products updated: $updated"

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$updated" -ge 1 ]; then
        success "Batch updated products (count=$updated)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Should have updated some products"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# ----------------------------------------------------------------------------
# Order Tests
# ----------------------------------------------------------------------------

test_list_orders() {
    header "List Orders"

    GET /api/orders
    expect_status 200 "List orders returns 200"
    expect_contains "orders" "Response contains orders"

    local count=$(json_val '.count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$count" -ge 10 ]; then
        success "Seed data has orders (count=$count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected at least 10 orders from seed data"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_list_orders_filtering() {
    header "List Orders - Filtering"

    GET "/api/orders?status=delivered"
    expect_status 200 "Filter by status returns 200"
    local delivered=$(json_val '.count')
    info "Delivered orders: $delivered"

    GET "/api/orders?email=john@example.com"
    expect_status 200 "Filter by email returns 200"
    local john_orders=$(json_val '.count')
    info "Orders for john@example.com: $john_orders"
}

test_get_order() {
    header "Get Single Order"

    GET /api/orders/1
    expect_status 200 "Get order returns 200"
    expect_contains "order" "Response contains order"
    expect_contains "items" "Response contains items"
    expect_json '.order.order_number' 'ORD-2024-0001' "Order number correct"
}

test_get_order_not_found() {
    header "Get Order - Not Found"

    GET /api/orders/99999
    expect_status 404 "Missing order returns 404"
}

test_create_order() {
    header "Create Order"

    POST /api/orders customer_email="test@shop.com" customer_name="Test User" shipping_address="123 Test St"
    expect_status 201 "Create order returns 201"
    expect_json '.success' 'true' "Create succeeds"
    expect_json '.status' 'pending' "Status is pending"

    local order_id=$(json_val '.id')
    info "Created order ID: $order_id"
}

test_add_order_item() {
    header "Add Item to Order"

    # Create a new order first
    POST /api/orders customer_email="additem@test.com" customer_name="Add Item Test" shipping_address="456 Item St"
    expect_status 201 "Create order for item test"
    local order_id=$(json_val '.id')

    # Add item to order
    POST "/api/orders/$order_id/items" product_id=1 quantity=1
    expect_status 201 "Add item returns 201"
    expect_json '.success' 'true' "Add item succeeds"
    expect_json '.quantity' '1' "Quantity is 1"
    expect_contains "unit_price" "Response includes unit_price"
    expect_contains "total" "Response includes total"
}

test_add_item_order_not_found() {
    header "Add Item - Order Not Found"

    POST "/api/orders/99999/items" product_id=1 quantity=1
    expect_status 404 "Missing order returns 404"
}

test_add_item_product_not_found() {
    header "Add Item - Product Not Found"

    # Use an existing pending order from seed data (order 5 is pending)
    POST "/api/orders/5/items" product_id=99999 quantity=1
    expect_status 404 "Missing product returns 404"
    expect_contains "Product not found" "Error message correct"
}

# ----------------------------------------------------------------------------
# Order State Machine Tests
# ----------------------------------------------------------------------------

test_order_state_pending_to_paid() {
    header "Order State: Pending -> Paid"

    # Create order
    POST /api/orders customer_email="pay@test.com" customer_name="Pay Test" shipping_address="Pay St"
    local order_id=$(json_val '.id')

    # Add item (required to pay)
    POST "/api/orders/$order_id/items" product_id=7 quantity=2
    expect_status 201 "Add item succeeds"

    # Pay order
    POST "/api/orders/$order_id/pay"
    expect_status 200 "Pay returns 200"
    expect_json '.success' 'true' "Pay succeeds"
    expect_json '.previous_status' 'pending' "Previous status was pending"
    expect_json '.new_status' 'paid' "New status is paid"
}

test_order_state_paid_to_shipped() {
    header "Order State: Paid -> Shipped"

    # Create and pay order
    POST /api/orders customer_email="ship@test.com" customer_name="Ship Test" shipping_address="Ship St"
    local order_id=$(json_val '.id')
    POST "/api/orders/$order_id/items" product_id=8 quantity=1
    POST "/api/orders/$order_id/pay"
    expect_json '.new_status' 'paid' "Order is paid"

    # Ship order
    POST "/api/orders/$order_id/ship"
    expect_status 200 "Ship returns 200"
    expect_json '.success' 'true' "Ship succeeds"
    expect_json '.previous_status' 'paid' "Previous status was paid"
    expect_json '.new_status' 'shipped' "New status is shipped"
}

test_order_state_shipped_to_delivered() {
    header "Order State: Shipped -> Delivered"

    # Create, pay, ship order
    POST /api/orders customer_email="deliver@test.com" customer_name="Deliver Test" shipping_address="Deliver St"
    local order_id=$(json_val '.id')
    POST "/api/orders/$order_id/items" product_id=10 quantity=1
    POST "/api/orders/$order_id/pay"
    POST "/api/orders/$order_id/ship"
    expect_json '.new_status' 'shipped' "Order is shipped"

    # Deliver order
    POST "/api/orders/$order_id/deliver"
    expect_status 200 "Deliver returns 200"
    expect_json '.success' 'true' "Deliver succeeds"
    expect_json '.previous_status' 'shipped' "Previous status was shipped"
    expect_json '.new_status' 'delivered' "New status is delivered"
}

test_order_state_cancel_pending() {
    header "Order State: Cancel Pending Order"

    # Create order
    POST /api/orders customer_email="cancel1@test.com" customer_name="Cancel Test 1" shipping_address="Cancel St 1"
    local order_id=$(json_val '.id')

    # Cancel
    POST "/api/orders/$order_id/cancel"
    expect_status 200 "Cancel returns 200"
    expect_json '.success' 'true' "Cancel succeeds"
    expect_json '.previous_status' 'pending' "Previous status was pending"
    expect_json '.new_status' 'cancelled' "New status is cancelled"
}

test_order_state_cancel_paid() {
    header "Order State: Cancel Paid Order (Restores Inventory)"

    # Get initial stock for product
    GET /api/products/11
    local initial_stock=$(json_val '.product.stock_qty')
    info "Initial stock for product 11: $initial_stock"

    # Create and pay order
    POST /api/orders customer_email="cancel2@test.com" customer_name="Cancel Test 2" shipping_address="Cancel St 2"
    local order_id=$(json_val '.id')
    POST "/api/orders/$order_id/items" product_id=11 quantity=2
    POST "/api/orders/$order_id/pay"
    expect_json '.new_status' 'paid' "Order is paid"

    # Check stock was deducted
    GET /api/products/11
    local after_pay_stock=$(json_val '.product.stock_qty')
    info "Stock after pay: $after_pay_stock"

    # Cancel (should restore inventory)
    POST "/api/orders/$order_id/cancel"
    expect_status 200 "Cancel returns 200"
    expect_json '.success' 'true' "Cancel succeeds"
    expect_json '.inventory_restored' 'true' "Inventory was restored"

    # Verify stock restored
    GET /api/products/11
    local restored_stock=$(json_val '.product.stock_qty')
    info "Stock after cancel: $restored_stock"

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$restored_stock" -eq "$initial_stock" ]; then
        success "Inventory correctly restored"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Inventory should be restored to $initial_stock, got $restored_stock"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_order_state_invalid_transitions() {
    header "Order State: Invalid Transitions"

    # Query for orders in specific states from seed data
    # Seed data in shopapp.yaml creates: order 1=delivered, 4=paid, 5=pending
    # If seed data changes, these IDs may need updating
    GET "/api/orders?status=delivered&limit=1"
    local delivered_id=$(json_val '.orders[0].id')

    GET "/api/orders?status=pending&limit=1"
    local pending_id=$(json_val '.orders[0].id')

    GET "/api/orders?status=paid&limit=1"
    local paid_id=$(json_val '.orders[0].id')

    # Try to pay already-delivered order
    POST "/api/orders/$delivered_id/pay"
    expect_status 400 "Pay delivered order returns 400"
    expect_contains "pending" "Error mentions pending status"

    # Try to ship pending order
    POST "/api/orders/$pending_id/ship"
    expect_status 400 "Ship pending order returns 400"
    expect_contains "paid" "Error mentions paid status"

    # Try to deliver paid order (needs to be shipped first)
    POST "/api/orders/$paid_id/deliver"
    expect_status 400 "Deliver paid order returns 400"
    expect_contains "shipped" "Error mentions shipped status"

    # Try to cancel delivered order
    POST "/api/orders/$delivered_id/cancel"
    expect_status 400 "Cancel delivered order returns 400"
    expect_contains "shipped or delivered" "Error mentions terminal states"
}

test_order_state_empty_order() {
    header "Order State: Cannot Pay Empty Order"

    # Create order without items
    POST /api/orders customer_email="empty@test.com" customer_name="Empty Test" shipping_address="Empty St"
    local order_id=$(json_val '.id')

    # Try to pay
    POST "/api/orders/$order_id/pay"
    expect_status 400 "Pay empty order returns 400"
    expect_contains "empty order" "Error mentions empty order"
}

# ----------------------------------------------------------------------------
# Inventory Tests
# ----------------------------------------------------------------------------

test_get_inventory() {
    header "Get Inventory"

    GET /api/inventory/1
    expect_status 200 "Get inventory returns 200"
    expect_contains "product" "Response contains product"
    expect_contains "current_stock" "Response contains current_stock"
    expect_contains "history" "Response contains history"

    local stock=$(json_val '.current_stock')
    info "Product 1 current stock: $stock"
}

test_get_inventory_not_found() {
    header "Get Inventory - Not Found"

    GET /api/inventory/99999
    expect_status 404 "Missing product returns 404"
}

test_adjust_inventory_add() {
    header "Adjust Inventory - Add Stock"

    # Get current stock
    GET /api/inventory/15
    local before=$(json_val '.current_stock')
    info "Stock before: $before"

    # Add stock
    POST /api/inventory/15/adjust change_qty=10 reason="Restock"
    expect_status 200 "Adjust returns 200"
    expect_json '.success' 'true' "Adjust succeeds"
    expect_json '.change_qty' '10' "Change qty is 10"

    # Verify
    GET /api/inventory/15
    local after=$(json_val '.current_stock')
    info "Stock after: $after"

    TESTS_RUN=$((TESTS_RUN + 1))
    local expected=$((before + 10))
    if [ "$after" -eq "$expected" ]; then
        success "Stock increased by 10 (was $before, now $after)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected stock $expected, got $after"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_adjust_inventory_remove() {
    header "Adjust Inventory - Remove Stock"

    # Get current stock
    GET /api/inventory/16
    local before=$(json_val '.current_stock')
    info "Stock before: $before"

    # Remove stock
    POST /api/inventory/16/adjust change_qty=-5 reason="Damaged goods"
    expect_status 200 "Adjust returns 200"
    expect_json '.success' 'true' "Adjust succeeds"
    expect_json '.change_qty' '-5' "Change qty is -5"

    # Verify
    GET /api/inventory/16
    local after=$(json_val '.current_stock')
    info "Stock after: $after"

    TESTS_RUN=$((TESTS_RUN + 1))
    local expected=$((before - 5))
    if [ "$after" -eq "$expected" ]; then
        success "Stock decreased by 5 (was $before, now $after)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected stock $expected, got $after"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

test_inventory_history() {
    header "Inventory History"

    # Make an adjustment to have history
    POST /api/inventory/17/adjust change_qty=3 reason="Test adjustment"

    # Get inventory with history
    GET /api/inventory/17
    expect_status 200 "Get inventory returns 200"

    local history_count=$(json_val '.history_count')
    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$history_count" -ge 1 ]; then
        success "Inventory has history (count=$history_count)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        fail "Expected at least 1 history entry"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# ----------------------------------------------------------------------------
# Stats Tests
# ----------------------------------------------------------------------------

test_shop_stats() {
    header "Shop Statistics"

    GET /api/stats
    expect_status 200 "Stats returns 200"
    expect_contains "products" "Response contains product stats"
    expect_contains "orders" "Response contains order stats"
    expect_contains "top_categories" "Response contains top categories"

    local total_products=$(json_val '.products.total_products')
    local total_orders=$(json_val '.orders.total_orders')
    info "Total products: $total_products, Total orders: $total_orders"
}

test_stats_step_caching() {
    header "Stats - Step-Level Caching"

    # Note: Stats may have been called already, so first request might be cached
    GET /api/stats
    local first_cached=$(json_val '.product_stats_cached')
    info "First request: product_stats_cached=$first_cached"

    GET /api/stats
    expect_json '.product_stats_cached' 'true' "Second request: product_stats_cached=true"
}

# ----------------------------------------------------------------------------
# Rate Limiting
# ----------------------------------------------------------------------------

test_rate_limiting() {
    header "Rate Limiting"

    # Reset rate limits to start with a full bucket
    reset_rate_limits "orders"

    info "Sending rapid requests to trigger rate limit..."
    EXPECT_RATE_LIMIT=true  # We expect 429s in this test
    local rate_limited=false
    # orders pool has burst=20, send 30 to reliably trigger
    for i in $(seq 1 30); do
        POST /api/orders customer_email="rate$i@test.com" customer_name="Rate Test $i" shipping_address="Rate St $i"
        if [ "$_status" = "429" ]; then
            rate_limited=true
            break
        fi
    done

    TESTS_RUN=$((TESTS_RUN + 1))
    if [ "$rate_limited" = true ]; then
        success "Rate limit triggered (429)"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        expect_header_exists "Retry-After" "Retry-After header present"
    else
        fail "Rate limit not triggered after 15 requests"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi

    reset_expectations
    reset_rate_limits "orders"  # Clean up for subsequent tests
}

# ============================================================================
# MAIN
# ============================================================================

main() {
    print_test_header "$APP_NAME" "E-commerce Application API - E2E Test Suite"

    check_dependencies
    build_binary
    create_config
    start_server

    # Run all tests
    test_health
    test_init_database

    # Category tests
    test_list_categories
    test_list_categories_caching
    test_get_category
    test_get_category_with_products
    test_get_category_not_found

    # Product tests
    test_list_products
    test_list_products_filtering
    test_list_products_pagination
    test_get_product
    test_get_product_caching
    test_get_product_not_found
    test_update_price
    test_update_price_not_found
    test_batch_update_prices

    # Order tests
    test_list_orders
    test_list_orders_filtering
    test_get_order
    test_get_order_not_found
    test_create_order
    test_add_order_item
    test_add_item_order_not_found
    test_add_item_product_not_found

    # Order state machine tests
    test_order_state_pending_to_paid
    test_order_state_paid_to_shipped
    test_order_state_shipped_to_delivered
    test_order_state_cancel_pending
    test_order_state_cancel_paid
    test_order_state_invalid_transitions
    test_order_state_empty_order

    # Inventory tests
    test_get_inventory
    test_get_inventory_not_found
    test_adjust_inventory_add
    test_adjust_inventory_remove
    test_inventory_history

    # Stats tests
    test_shop_stats
    test_stats_step_caching

    # Rate limiting
    test_rate_limiting

    # Standard endpoints
    test_metrics
    test_openapi

    # Summary
    print_summary
    print_coverage_info
    exit_with_result
}

main
