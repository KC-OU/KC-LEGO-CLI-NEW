-- Structure only (no rows) of the live ModernWMS wms.db, captured 2026-09-18.
-- Refresh it when ModernWMS is upgraded: docker exec ... select sql from sqlite_master.
CREATE TABLE "__EFMigrationsHistory" (
    "MigrationId" TEXT NOT NULL CONSTRAINT "PK___EFMigrationsHistory" PRIMARY KEY,
    "ProductVersion" TEXT NOT NULL
);
CREATE TABLE "asn" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_asn" PRIMARY KEY AUTOINCREMENT,
    "asn_no" TEXT NOT NULL,
    "asn_status" INTEGER NOT NULL,
    "spu_id" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "asn_qty" INTEGER NOT NULL,
    "actual_qty" INTEGER NOT NULL,
    "sorted_qty" INTEGER NOT NULL,
    "shortage_qty" INTEGER NOT NULL,
    "more_qty" INTEGER NOT NULL,
    "damage_qty" INTEGER NOT NULL,
    "weight" TEXT NOT NULL,
    "volume" TEXT NOT NULL,
    "supplier_id" INTEGER NOT NULL,
    "supplier_name" TEXT NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_owner_name" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "asnsort" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_asnsort" PRIMARY KEY AUTOINCREMENT,
    "asn_id" INTEGER NOT NULL,
    "sorted_qty" INTEGER NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "category" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_category" PRIMARY KEY AUTOINCREMENT,
    "category_name" TEXT NOT NULL,
    "parent_id" INTEGER NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "company" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_company" PRIMARY KEY AUTOINCREMENT,
    "company_name" TEXT NOT NULL,
    "city" TEXT NOT NULL,
    "address" TEXT NOT NULL,
    "manager" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "customer" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_customer" PRIMARY KEY AUTOINCREMENT,
    "customer_name" TEXT NOT NULL,
    "city" TEXT NOT NULL,
    "address" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "manager" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "dispatchlist" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_dispatchlist" PRIMARY KEY AUTOINCREMENT,
    "dispatch_no" TEXT NOT NULL,
    "dispatch_status" INTEGER NOT NULL,
    "customer_id" INTEGER NOT NULL,
    "customer_name" TEXT NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "qty" INTEGER NOT NULL,
    "weight" TEXT NOT NULL,
    "volume" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "damage_qty" INTEGER NOT NULL,
    "lock_qty" INTEGER NOT NULL,
    "picked_qty" INTEGER NOT NULL,
    "intrasit_qty" INTEGER NOT NULL,
    "package_qty" INTEGER NOT NULL,
    "weighing_qty" INTEGER NOT NULL,
    "actual_qty" INTEGER NOT NULL,
    "sign_qty" INTEGER NOT NULL,
    "package_no" TEXT NOT NULL,
    "package_person" TEXT NOT NULL,
    "package_time" TEXT NOT NULL,
    "weighing_no" TEXT NOT NULL,
    "weighing_person" TEXT NOT NULL,
    "weighing_weight" TEXT NOT NULL,
    "waybill_no" TEXT NOT NULL,
    "carrier" TEXT NOT NULL,
    "freightfee" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "dispatchpicklist" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_dispatchpicklist" PRIMARY KEY AUTOINCREMENT,
    "dispatchlist_id" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "pick_qty" INTEGER NOT NULL,
    "picked_qty" INTEGER NOT NULL,
    "is_update_stock" INTEGER NOT NULL,
    "last_update_time" TEXT NOT NULL,
    CONSTRAINT "FK_dispatchpicklist_dispatchlist_dispatchlist_id" FOREIGN KEY ("dispatchlist_id") REFERENCES "dispatchlist" ("id") ON DELETE CASCADE
);
CREATE TABLE "freightfee" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_freightfee" PRIMARY KEY AUTOINCREMENT,
    "carrier" TEXT NOT NULL,
    "departure_city" TEXT NOT NULL,
    "arrival_city" TEXT NOT NULL,
    "price_per_weight" TEXT NOT NULL,
    "price_per_volume" TEXT NOT NULL,
    "min_payment" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "goodslocation" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_goodslocation" PRIMARY KEY AUTOINCREMENT,
    "warehouse_id" INTEGER NOT NULL,
    "warehouse_name" TEXT NOT NULL,
    "warehouse_area_name" TEXT NOT NULL,
    "warehouse_area_property" INTEGER NOT NULL,
    "location_name" TEXT NOT NULL,
    "location_length" TEXT NOT NULL,
    "location_width" TEXT NOT NULL,
    "location_heigth" TEXT NOT NULL,
    "location_volume" TEXT NOT NULL,
    "location_load" TEXT NOT NULL,
    "roadway_number" TEXT NOT NULL,
    "shelf_number" TEXT NOT NULL,
    "layer_number" TEXT NOT NULL,
    "tag_number" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL,
    "warehouse_area_id" INTEGER NOT NULL
);
CREATE TABLE "goodsowner" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_goodsowner" PRIMARY KEY AUTOINCREMENT,
    "goods_owner_name" TEXT NOT NULL,
    "city" TEXT NOT NULL,
    "address" TEXT NOT NULL,
    "manager" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "menu" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_menu" PRIMARY KEY AUTOINCREMENT,
    "menu_name" TEXT NOT NULL,
    "module" TEXT NOT NULL,
    "vue_path" TEXT NOT NULL,
    "vue_path_detail" TEXT NOT NULL,
    "vue_directory" TEXT NOT NULL,
    "sort" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "rolemenu" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_rolemenu" PRIMARY KEY AUTOINCREMENT,
    "userrole_id" INTEGER NOT NULL,
    "menu_id" INTEGER NOT NULL,
    "authority" INTEGER NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "sku" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_sku" PRIMARY KEY AUTOINCREMENT,
    "spu_id" INTEGER NOT NULL,
    "sku_code" TEXT NOT NULL,
    "sku_name" TEXT NOT NULL,
    "weight" TEXT NOT NULL,
    "lenght" TEXT NOT NULL,
    "width" TEXT NOT NULL,
    "height" TEXT NOT NULL,
    "volume" TEXT NOT NULL,
    "unit" TEXT NOT NULL,
    "cost" TEXT NOT NULL,
    "price" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    CONSTRAINT "FK_sku_spu_spu_id" FOREIGN KEY ("spu_id") REFERENCES "spu" ("id") ON DELETE CASCADE
);
CREATE TABLE "spu" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_spu" PRIMARY KEY AUTOINCREMENT,
    "spu_code" TEXT NOT NULL,
    "spu_name" TEXT NOT NULL,
    "category_id" INTEGER NOT NULL,
    "spu_description" TEXT NOT NULL,
    "bar_code" TEXT NOT NULL,
    "supplier_id" INTEGER NOT NULL,
    "supplier_name" TEXT NOT NULL,
    "brand" TEXT NOT NULL,
    "origin" TEXT NOT NULL,
    "length_unit" INTEGER NOT NULL,
    "volume_unit" INTEGER NOT NULL,
    "weight_unit" INTEGER NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "stock" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stock" PRIMARY KEY AUTOINCREMENT,
    "sku_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "qty" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "is_freeze" INTEGER NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "stockadjust" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stockadjust" PRIMARY KEY AUTOINCREMENT,
    "job_code" TEXT NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "qty" INTEGER NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL,
    "is_update_stock" INTEGER NOT NULL,
    "job_type" INTEGER NOT NULL,
    "source_table_id" INTEGER NOT NULL
);
CREATE TABLE "stockfreeze" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stockfreeze" PRIMARY KEY AUTOINCREMENT,
    "job_code" TEXT NOT NULL,
    "job_type" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "handler" TEXT NOT NULL,
    "handle_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "stockmove" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stockmove" PRIMARY KEY AUTOINCREMENT,
    "job_code" TEXT NOT NULL,
    "move_status" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "orig_goods_location_id" INTEGER NOT NULL,
    "dest_googs_location_id" INTEGER NOT NULL,
    "qty" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "handler" TEXT NOT NULL,
    "handle_time" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "stockprocess" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stockprocess" PRIMARY KEY AUTOINCREMENT,
    "job_code" TEXT NOT NULL,
    "job_type" INTEGER NOT NULL,
    "process_status" INTEGER NOT NULL,
    "processor" TEXT NOT NULL,
    "process_time" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "stockprocessdetail" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stockprocessdetail" PRIMARY KEY AUTOINCREMENT,
    "stock_process_id" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "qty" INTEGER NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL,
    "is_source" INTEGER NOT NULL,
    "is_update_stock" INTEGER NOT NULL,
    CONSTRAINT "FK_stockprocessdetail_stockprocess_stock_process_id" FOREIGN KEY ("stock_process_id") REFERENCES "stockprocess" ("id") ON DELETE CASCADE
);
CREATE TABLE "stocktaking" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_stocktaking" PRIMARY KEY AUTOINCREMENT,
    "job_code" TEXT NOT NULL,
    "job_status" INTEGER NOT NULL,
    "sku_id" INTEGER NOT NULL,
    "goods_owner_id" INTEGER NOT NULL,
    "goods_location_id" INTEGER NOT NULL,
    "book_qty" INTEGER NOT NULL,
    "counted_qty" INTEGER NOT NULL,
    "difference_qty" INTEGER NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL,
    "handler" TEXT NOT NULL,
    "handle_time" TEXT NOT NULL
);
CREATE TABLE "supplier" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_supplier" PRIMARY KEY AUTOINCREMENT,
    "supplier_name" TEXT NOT NULL,
    "city" TEXT NOT NULL,
    "address" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "manager" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "user" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_user" PRIMARY KEY AUTOINCREMENT,
    "user_num" TEXT NOT NULL,
    "user_name" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "user_role" TEXT NOT NULL,
    "sex" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "auth_string" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE user_security (
        user_id INTEGER PRIMARY KEY,
        must_change_pw INTEGER DEFAULT 0,
        temp_pw_created_at TEXT
    );
CREATE TABLE "userrole" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_userrole" PRIMARY KEY AUTOINCREMENT,
    "role_name" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "warehouse" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_warehouse" PRIMARY KEY AUTOINCREMENT,
    "warehouse_name" TEXT NOT NULL,
    "city" TEXT NOT NULL,
    "address" TEXT NOT NULL,
    "email" TEXT NOT NULL,
    "manager" TEXT NOT NULL,
    "contact_tel" TEXT NOT NULL,
    "creator" TEXT NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL
);
CREATE TABLE "warehousearea" (
    "id" INTEGER NOT NULL CONSTRAINT "PK_warehousearea" PRIMARY KEY AUTOINCREMENT,
    "warehouse_id" INTEGER NOT NULL,
    "area_name" TEXT NOT NULL,
    "parent_id" INTEGER NOT NULL,
    "create_time" TEXT NOT NULL,
    "last_update_time" TEXT NOT NULL,
    "is_valid" INTEGER NOT NULL,
    "tenant_id" INTEGER NOT NULL,
    "area_property" INTEGER NOT NULL
);
CREATE INDEX "IX_dispatchpicklist_dispatchlist_id" ON "dispatchpicklist" ("dispatchlist_id");
CREATE INDEX "IX_sku_spu_id" ON "sku" ("spu_id");
CREATE INDEX "IX_stockprocessdetail_stock_process_id" ON "stockprocessdetail" ("stock_process_id");
