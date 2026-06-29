CREATE TABLE "accounts" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"name" text NOT NULL,
	"type" text NOT NULL,
	"starting_balance_cents" bigint DEFAULT 0 NOT NULL,
	"credit_limit_cents" bigint,
	"apr_bps" bigint,
	"monthly_payment_cents" bigint,
	"include_in_paydown" boolean DEFAULT false NOT NULL,
	"payment_category_id" bigint,
	"archived_at" timestamp with time zone,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	CONSTRAINT "accounts_user_name_key" UNIQUE("user_id","name")
);
--> statement-breakpoint
CREATE TABLE "app_settings" (
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"key" text NOT NULL,
	"value" text NOT NULL,
	CONSTRAINT "app_settings_user_id_key_pk" PRIMARY KEY("user_id","key")
);
--> statement-breakpoint
CREATE TABLE "budgets" (
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"month" text NOT NULL,
	"category_id" bigint NOT NULL,
	"assigned_cents" bigint DEFAULT 0 NOT NULL,
	CONSTRAINT "budgets_month_category_id_pk" PRIMARY KEY("month","category_id")
);
--> statement-breakpoint
CREATE TABLE "categories" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"group_id" bigint NOT NULL,
	"name" text NOT NULL,
	"goal_cents" bigint,
	"goal_due_date" date,
	"sort_order" bigint DEFAULT 0 NOT NULL,
	"is_income" boolean DEFAULT false NOT NULL,
	"archived_at" timestamp with time zone,
	CONSTRAINT "categories_group_name_key" UNIQUE("group_id","name")
);
--> statement-breakpoint
CREATE TABLE "category_groups" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"name" text NOT NULL,
	"sort_order" bigint DEFAULT 0 NOT NULL,
	CONSTRAINT "category_groups_user_name_key" UNIQUE("user_id","name")
);
--> statement-breakpoint
CREATE TABLE "incomes" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"month" text NOT NULL,
	"name" text NOT NULL,
	"amount_cents" bigint DEFAULT 0 NOT NULL,
	"sort_order" bigint DEFAULT 0 NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	CONSTRAINT "incomes_user_month_name_key" UNIQUE("user_id","month","name")
);
--> statement-breakpoint
CREATE TABLE "transactions" (
	"id" bigserial PRIMARY KEY NOT NULL,
	"user_id" uuid DEFAULT '00000000-0000-0000-0000-000000000001' NOT NULL,
	"date" date NOT NULL,
	"account_id" bigint NOT NULL,
	"category_id" bigint,
	"transfer_account_id" bigint,
	"transfer_pair_id" bigint,
	"payee" text,
	"notes" text,
	"outflow_cents" bigint DEFAULT 0 NOT NULL,
	"inflow_cents" bigint DEFAULT 0 NOT NULL,
	"cleared" boolean DEFAULT false NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL
);
--> statement-breakpoint
ALTER TABLE "budgets" ADD CONSTRAINT "budgets_category_id_categories_id_fk" FOREIGN KEY ("category_id") REFERENCES "public"."categories"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "categories" ADD CONSTRAINT "categories_group_id_category_groups_id_fk" FOREIGN KEY ("group_id") REFERENCES "public"."category_groups"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "transactions" ADD CONSTRAINT "transactions_account_id_accounts_id_fk" FOREIGN KEY ("account_id") REFERENCES "public"."accounts"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "transactions" ADD CONSTRAINT "transactions_category_id_categories_id_fk" FOREIGN KEY ("category_id") REFERENCES "public"."categories"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "transactions" ADD CONSTRAINT "transactions_transfer_account_id_accounts_id_fk" FOREIGN KEY ("transfer_account_id") REFERENCES "public"."accounts"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
CREATE INDEX "idx_accounts_user" ON "accounts" USING btree ("user_id");--> statement-breakpoint
CREATE INDEX "idx_budgets_user" ON "budgets" USING btree ("user_id");--> statement-breakpoint
CREATE INDEX "idx_categories_user" ON "categories" USING btree ("user_id");--> statement-breakpoint
CREATE INDEX "idx_category_groups_user" ON "category_groups" USING btree ("user_id");--> statement-breakpoint
CREATE INDEX "idx_incomes_user" ON "incomes" USING btree ("user_id");--> statement-breakpoint
CREATE INDEX "idx_incomes_month" ON "incomes" USING btree ("month");--> statement-breakpoint
CREATE INDEX "idx_tx_account_date" ON "transactions" USING btree ("account_id","date");--> statement-breakpoint
CREATE INDEX "idx_tx_category_date" ON "transactions" USING btree ("category_id","date");--> statement-breakpoint
CREATE INDEX "idx_transactions_user" ON "transactions" USING btree ("user_id");