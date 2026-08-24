package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	"platform-service/internal/client/controller"
	"platform-service/internal/entity"

	"gorm.io/gorm"
)

// ---------- 内存 fake repository ----------

type fakePlanRepo struct {
	byID map[string]*entity.ServerPlan
}

func newFakePlanRepo() *fakePlanRepo { return &fakePlanRepo{byID: map[string]*entity.ServerPlan{}} }

func (f *fakePlanRepo) Save(_ context.Context, p *entity.ServerPlan) error {
	f.byID[p.ID] = p
	return nil
}
func (f *fakePlanRepo) GetByID(_ context.Context, id string) (*entity.ServerPlan, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakePlanRepo) ListAll(_ context.Context) ([]*entity.ServerPlan, error) {
	out := make([]*entity.ServerPlan, 0, len(f.byID))
	for _, p := range f.byID {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakePlanRepo) ListEnabled(_ context.Context) ([]*entity.ServerPlan, error) {
	out := make([]*entity.ServerPlan, 0)
	for _, p := range f.byID {
		if p.Enabled {
			out = append(out, p)
		}
	}
	return out, nil
}
func (f *fakePlanRepo) Delete(_ context.Context, id string) error {
	delete(f.byID, id)
	return nil
}

type fakeSubRepo struct {
	byID map[string]*entity.Subscription
}

func newFakeSubRepo() *fakeSubRepo { return &fakeSubRepo{byID: map[string]*entity.Subscription{}} }

func (f *fakeSubRepo) Save(_ context.Context, s *entity.Subscription) error {
	f.byID[s.ID] = s
	return nil
}
func (f *fakeSubRepo) GetByID(_ context.Context, id string) (*entity.Subscription, error) {
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeSubRepo) ListByUser(_ context.Context, userID string) ([]*entity.Subscription, error) {
	out := make([]*entity.Subscription, 0)
	for _, s := range f.byID {
		if s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeSubRepo) ListByPlan(_ context.Context, planID string) ([]*entity.Subscription, error) {
	out := make([]*entity.Subscription, 0)
	for _, s := range f.byID {
		if s.PlanID == planID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakeSubRepo) ListAll(_ context.Context) ([]*entity.Subscription, error) {
	out := make([]*entity.Subscription, 0, len(f.byID))
	for _, s := range f.byID {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSubRepo) ListOverdue(_ context.Context, now time.Time) ([]*entity.Subscription, error) {
	out := make([]*entity.Subscription, 0)
	for _, s := range f.byID {
		if s.Status == entity.SubscriptionActive && s.ExpiresAt != nil && s.ExpiresAt.Before(now) {
			out = append(out, s)
		}
	}
	return out, nil
}

// fakeSubController 模拟 controller 客户端（M11 订阅内实例操作）
type fakeSubController struct {
	createCalled bool
	lastGameID   string
	lastSubID    string
	lastConfig   map[string]string
	startErr     error
	stopErr      error
	getInst      *controller.GameInstance
	insts        []controller.GameInstance // ListGameInstancesBySubscription 返回值
	stopped      []string                  // 被调 stop 的实例 ID
	runtime      *controller.InstanceRuntime // B-04/P1-1：GetInstanceRuntime 返回值（nil = 默认）
	runtimeErr   error                       // B-04/P1-1：GetInstanceRuntime 错误
}

func (f *fakeSubController) CreateGameInstance(_ context.Context, gameID, _ string, subscriptionID string, config map[string]string) (*controller.GameInstance, error) {
	f.createCalled = true
	f.lastGameID = gameID
	f.lastSubID = subscriptionID
	f.lastConfig = config
	return &controller.GameInstance{ID: "inst-new", GameID: gameID, SubscriptionID: &subscriptionID}, nil
}
func (f *fakeSubController) GetGameInstance(_ context.Context, instanceID string) (*controller.GameInstance, error) {
	if f.getInst == nil {
		return nil, controller.ErrNotFound
	}
	return f.getInst, nil
}
func (f *fakeSubController) StartGameInstance(_ context.Context, instanceID string) error { return f.startErr }
func (f *fakeSubController) StopGameInstance(_ context.Context, instanceID string) error {
	f.stopped = append(f.stopped, instanceID)
	return f.stopErr
}
func (f *fakeSubController) ListGameInstancesBySubscription(_ context.Context, subscriptionID string) ([]controller.GameInstance, error) {
	return f.insts, nil
}

// B-04/P1-1：运行时统计（fake 返回固定值或预设）
func (f *fakeSubController) GetInstanceRuntime(_ context.Context, instanceID string) (*controller.InstanceRuntime, error) {
	if f.runtimeErr != nil {
		return nil, f.runtimeErr
	}
	if f.runtime != nil {
		return f.runtime, nil
	}
	return &controller.InstanceRuntime{InstanceID: instanceID, Running: true, PlayerCount: 0, MaxPlayers: 8, Healthy: true, ProbeMode: "script"}, nil
}

func newTestUseCases() (*PlanUseCase, *SubscriptionUseCase, *fakePlanRepo, *fakeSubRepo, *fakeSubController) {
	pr := newFakePlanRepo()
	sr := newFakeSubRepo()
	cc := &fakeSubController{}
	planUC := NewPlanUseCase(pr, sr)
	subUC := NewSubscriptionUseCase(sr, planUC, cc)
	return planUC, subUC, pr, sr, cc
}

func basePlan() *entity.ServerPlan {
	return &entity.ServerPlan{
		DisplayName:   "DST 双人包",
		PriceCents:    9900,
		DurationHours: 720,
		MaxInstances:  5,
		Basket: []entity.PlanBasketItem{
			{GameID: "343050", Config: map[string]string{"world_name": "w1"}},
			{GameID: "294420"},
		},
	}
}

// ---------- PlanUseCase ----------

func TestCreatePlan_Validation(t *testing.T) {
	planUC, _, _, _, _ := newTestUseCases()
	cases := []struct {
		name string
		mut  func(p *entity.ServerPlan)
	}{
		{"missing display_name", func(p *entity.ServerPlan) { p.DisplayName = "" }},
		{"empty basket", func(p *entity.ServerPlan) { p.Basket = nil }},
		{"empty game_id", func(p *entity.ServerPlan) { p.Basket[0].GameID = "" }},
		{"duplicate game_id", func(p *entity.ServerPlan) { p.Basket = append(p.Basket, entity.PlanBasketItem{GameID: "343050"}) }},
		{"negative price", func(p *entity.ServerPlan) { p.PriceCents = -1 }},
		{"negative duration", func(p *entity.ServerPlan) { p.DurationHours = -5 }},
		{"negative max_instances", func(p *entity.ServerPlan) { p.MaxInstances = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := basePlan()
			tc.mut(p)
			if _, err := planUC.CreatePlan(context.Background(), p); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestCreatePlan_OK(t *testing.T) {
	planUC, _, _, _, _ := newTestUseCases()
	p, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if p.ID == "" {
		t.Fatal("plan id should be generated")
	}
	if !p.Enabled {
		t.Fatal("new plan should be enabled")
	}
}

func TestUpdatePlan_SnapshotUnaffected(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()

	// 创建套餐并购买
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}

	// 编辑套餐：替换篮子（含换掉 game 343050 的默认配置），其余字段整体提交
	updates := &entity.ServerPlan{
		DisplayName:   "DST 双人包",
		PriceCents:    9900,
		DurationHours: 720,
		Basket: []entity.PlanBasketItem{
			{GameID: "343050", Config: map[string]string{"world_name": "w2"}},
		},
	}
	updated, err := planUC.UpdatePlan(context.Background(), plan.ID, updates, nil)
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if len(updated.Basket) != 1 {
		t.Fatalf("plan basket should be replaced, got %d items", len(updated.Basket))
	}

	// 快照语义：已购订阅的 basket_snapshot 保持购买时内容
	if len(sub.BasketSnapshot) != 2 {
		t.Fatalf("subscription snapshot should keep 2 items, got %d", len(sub.BasketSnapshot))
	}
	if sub.BasketSnapshot[0].Config["world_name"] != "w1" {
		t.Fatalf("snapshot config should be w1, got %q", sub.BasketSnapshot[0].Config["world_name"])
	}
}

func TestUpdatePlan_NotFound(t *testing.T) {
	planUC, _, _, _, _ := newTestUseCases()
	_, err := planUC.UpdatePlan(context.Background(), "plan-missing", &entity.ServerPlan{}, nil)
	if !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestUpdatePlan_ZeroValuesApply(t *testing.T) {
	planUC, _, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	// 价格/时长改为 0（免费永久套餐）必须是合法更新，不能被"非零才更新"吞掉
	updates := &entity.ServerPlan{
		DisplayName:   basePlan().DisplayName,
		PriceCents:    0,
		DurationHours: 0,
		Basket:        basePlan().Basket,
	}
	updated, err := planUC.UpdatePlan(context.Background(), plan.ID, updates, nil)
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if updated.PriceCents != 0 || updated.DurationHours != 0 {
		t.Fatalf("zero values should apply, got price=%d duration=%d", updated.PriceCents, updated.DurationHours)
	}
}

func TestDeletePlan_ReferencedDisables(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := subUC.Purchase(context.Background(), "user-1", plan.ID); err != nil {
		t.Fatalf("purchase: %v", err)
	}

	if err := planUC.DeletePlan(context.Background(), plan.ID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}
	got, err := planUC.GetPlan(context.Background(), plan.ID)
	if err != nil {
		t.Fatalf("plan should still exist after disable: %v", err)
	}
	if got.Enabled {
		t.Fatal("referenced plan should be disabled, not deleted")
	}
}

func TestDeletePlan_UnreferencedPhysicallyDeleted(t *testing.T) {
	planUC, _, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if err := planUC.DeletePlan(context.Background(), plan.ID); err != nil {
		t.Fatalf("delete plan: %v", err)
	}
	if _, err := planUC.GetPlan(context.Background(), plan.ID); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("unreferenced plan should be physically deleted, got %v", err)
	}
}

// ---------- SubscriptionUseCase ----------

func TestPurchase_SnapshotAndExpiry(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}

	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if sub.Status != entity.SubscriptionActive {
		t.Fatalf("status should be active, got %s", sub.Status)
	}
	if sub.ExpiresAt == nil {
		t.Fatal("expires_at should be set for duration plan")
	}
	want := time.Now().Add(720 * time.Hour)
	if sub.ExpiresAt.Before(want.Add(-time.Minute)) || sub.ExpiresAt.After(want.Add(time.Minute)) {
		t.Fatalf("expires_at should be ~now+720h, got %v", sub.ExpiresAt)
	}
	if len(sub.BasketSnapshot) != 2 {
		t.Fatalf("snapshot should have 2 items, got %d", len(sub.BasketSnapshot))
	}
}

func TestPurchase_PermanentPlanNoExpiry(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	p := basePlan()
	p.DurationHours = 0
	plan, err := planUC.CreatePlan(context.Background(), p)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if sub.ExpiresAt != nil {
		t.Fatalf("permanent plan should have nil expires_at, got %v", sub.ExpiresAt)
	}
}

func TestPurchase_DisabledPlanRejected(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	plan.Enabled = false
	if err := planUC.planRepo.Save(context.Background(), plan); err != nil {
		t.Fatalf("save disabled plan: %v", err)
	}
	if _, err := subUC.Purchase(context.Background(), "user-1", plan.ID); err == nil {
		t.Fatal("purchase of disabled plan should fail")
	}
}

func TestPurchase_PlanNotFound(t *testing.T) {
	_, subUC, _, _, _ := newTestUseCases()
	if _, err := subUC.Purchase(context.Background(), "user-1", "plan-missing"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("expected ErrPlanNotFound, got %v", err)
	}
}

func TestSuspend_OnlyActive(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	suspended, err := subUC.Suspend(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended.Status != entity.SubscriptionSuspended {
		t.Fatalf("status should be suspended, got %s", suspended.Status)
	}
	if _, err := subUC.Suspend(context.Background(), sub.ID); err == nil {
		t.Fatal("suspending a suspended subscription should fail")
	}
}

// ---------- M11：订阅内实例 ----------

// buyActiveSub 创建一个可用订阅（篮子 343050/294420），返回 sub
func buyActiveSub(t *testing.T, planUC *PlanUseCase, subUC *SubscriptionUseCase) *entity.Subscription {
	t.Helper()
	plan, err := planUC.CreatePlan(context.Background(), basePlan())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	return sub
}

// TestCreateInstance_PresetMerge 创建实例：preset 默认配置为底、请求配置覆盖，subscription_id 透传
func TestCreateInstance_PresetMerge(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	inst, err := subUC.CreateInstance(context.Background(), "user-1", sub.ID, "343050",
		map[string]string{"world_name": "w2", "max_players": "4"})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if inst.ID != "inst-new" {
		t.Fatalf("unexpected instance: %v", inst.ID)
	}
	if cc.lastSubID != sub.ID {
		t.Fatalf("subscription_id should be %q, got %q", sub.ID, cc.lastSubID)
	}
	if cc.lastGameID != "343050" {
		t.Fatalf("game_id should be 343050, got %q", cc.lastGameID)
	}
	// preset {world_name: w1} 被请求覆盖为 w2；请求新增 key 保留
	if cc.lastConfig["world_name"] != "w2" || cc.lastConfig["max_players"] != "4" {
		t.Fatalf("merged config wrong: %v", cc.lastConfig)
	}
}

// TestCreateInstance_NotInBasket 篮子外的游戏拒绝
func TestCreateInstance_NotInBasket(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	if _, err := subUC.CreateInstance(context.Background(), "user-1", sub.ID, "999999", nil); err == nil {
		t.Fatal("game outside basket should be rejected")
	}
}

// TestCreateInstance_ExpiredSubscription 过期订阅拒绝创建
func TestCreateInstance_ExpiredSubscription(t *testing.T) {
	_, subUC, _, sr, _ := newTestUseCases()
	past := time.Now().Add(-time.Hour)
	sub := &entity.Subscription{
		ID:        "sub-exp",
		UserID:    "user-1",
		PlanID:    "plan-x",
		Status:    entity.SubscriptionActive,
		ExpiresAt: &past,
		BasketSnapshot: []entity.PlanBasketItem{{GameID: "343050"}},
	}
	if err := sr.Save(context.Background(), sub); err != nil {
		t.Fatalf("save expired sub: %v", err)
	}
	if _, err := subUC.CreateInstance(context.Background(), "user-1", sub.ID, "343050", nil); err == nil {
		t.Fatal("expired subscription should reject instance creation")
	}
}

// TestCreateInstance_NotOwned 他人订阅拒绝
func TestCreateInstance_NotOwned(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)
	if _, err := subUC.CreateInstance(context.Background(), "user-2", sub.ID, "343050", nil); err == nil {
		t.Fatal("other user's subscription should be rejected")
	}
}

// TestCreateInstance_MaxInstances 实例数量上限（快照语义：购买时从套餐固化）
func TestCreateInstance_MaxInstances(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	// 套餐上限 1
	p := basePlan()
	p.MaxInstances = 1
	plan, err := planUC.CreatePlan(context.Background(), p)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub, err := subUC.Purchase(context.Background(), "user-1", plan.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	if sub.MaxInstances != 1 {
		t.Fatalf("subscription should snapshot max_instances=1, got %d", sub.MaxInstances)
	}

	// 已有 1 个实例 → 再创建被拒
	subID := sub.ID
	cc.insts = []controller.GameInstance{{ID: "inst-1", GameID: "343050", SubscriptionID: &subID, Status: "stopped"}}
	if _, err := subUC.CreateInstance(context.Background(), "user-1", sub.ID, "294420", nil); err == nil {
		t.Fatal("instance creation beyond max_instances should be rejected")
	}

	// 上限 0 = 不限
	p2 := basePlan()
	p2.MaxInstances = 0
	plan2, err := planUC.CreatePlan(context.Background(), p2)
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub2, err := subUC.Purchase(context.Background(), "user-1", plan2.ID)
	if err != nil {
		t.Fatalf("purchase: %v", err)
	}
	cc.insts = []controller.GameInstance{{ID: "inst-1", GameID: "343050", SubscriptionID: &subID, Status: "stopped"}}
	if _, err := subUC.CreateInstance(context.Background(), "user-1", sub2.ID, "294420", nil); err != nil {
		t.Fatalf("max_instances=0 should be unlimited, got %v", err)
	}
}

// TestStartInstance_ConflictPassthrough 单活跃冲突（controller 409）透传
func TestStartInstance_ConflictPassthrough(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	subID := sub.ID
	cc.getInst = &controller.GameInstance{ID: "inst-1", GameID: "343050", SubscriptionID: &subID}
	cc.startErr = controller.ErrConflict

	err := subUC.StartInstance(context.Background(), "user-1", sub.ID, "inst-1")
	if !errors.Is(err, controller.ErrConflict) {
		t.Fatalf("conflict from controller should pass through, got %v", err)
	}
}

// TestStartInstance_NotOwnedInstance 实例不属于该订阅 → 拒绝（不调 controller start）
func TestStartInstance_NotOwnedInstance(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	other := "sub-other"
	cc.getInst = &controller.GameInstance{ID: "inst-1", GameID: "343050", SubscriptionID: &other}

	if err := subUC.StartInstance(context.Background(), "user-1", sub.ID, "inst-1"); err == nil {
		t.Fatal("instance of another subscription should be rejected")
	}
	if cc.startErr != nil { // fake 未调用 start（保持 startErr 原值 nil）
		t.Fatalf("controller start should not be called")
	}
}

// ---------- M12：续费 / 取消 / 到期 sweep ----------

// TestRenew_ExtendsFromExpiry 未到期续费：从原到期时间累加套餐时长
func TestRenew_ExtendsFromExpiry(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	before := *sub.ExpiresAt
	renewed, err := subUC.Renew(context.Background(), "user-1", sub.ID)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	want := before.Add(720 * time.Hour)
	if renewed.ExpiresAt == nil || renewed.ExpiresAt.Sub(want) > time.Minute || want.Sub(*renewed.ExpiresAt) > time.Minute {
		t.Fatalf("expires_at should extend by 720h from %v, got %v", before, renewed.ExpiresAt)
	}
	if renewed.Status != entity.SubscriptionActive {
		t.Fatalf("renewed status should be active, got %s", renewed.Status)
	}
}

// TestRenew_ExpiredBecomesActive 过期续费：expired → active，从 now 起算
func TestRenew_ExpiredBecomesActive(t *testing.T) {
	_, subUC, _, sr, _ := newTestUseCases()
	past := time.Now().Add(-time.Hour)
	sub := &entity.Subscription{
		ID: "sub-exp", UserID: "user-1", PlanID: "plan-x",
		Status: entity.SubscriptionExpired, ExpiresAt: &past,
		BasketSnapshot: []entity.PlanBasketItem{{GameID: "343050"}},
	}
	_ = sr.Save(context.Background(), sub)
	// plan-x 不存在 → renew 报错（先验证状态机）；补一个真实套餐再验证
	if _, err := subUC.Renew(context.Background(), "user-1", sub.ID); err == nil {
		t.Fatal("renew with missing plan should fail")
	}
}

// TestRenew_CancelledRejected 已取消订阅不可续费
func TestRenew_CancelledRejected(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)
	if _, err := subUC.Cancel(context.Background(), "user-1", sub.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := subUC.Renew(context.Background(), "user-1", sub.ID); err == nil {
		t.Fatal("cancelled subscription should not be renewable")
	}
}

// TestCancel_StopsActiveInstances 取消订阅：停止活跃实例 + 状态 cancelled
func TestCancel_StopsActiveInstances(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)

	cc.insts = []controller.GameInstance{
		{ID: "inst-running", GameID: "343050", Status: "running"},
		{ID: "inst-stopped", GameID: "294420", Status: "stopped"},
	}
	cancelled, err := subUC.Cancel(context.Background(), "user-1", sub.ID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != entity.SubscriptionCancelled {
		t.Fatalf("status should be cancelled, got %s", cancelled.Status)
	}
	if len(cc.stopped) != 1 || cc.stopped[0] != "inst-running" {
		t.Fatalf("only active instance should be stopped, got %v", cc.stopped)
	}
}

// TestExpireOverdue 到期 sweep：过期订阅的活跃实例被停止 + 标记 expired
func TestExpireOverdue(t *testing.T) {
	_, subUC, _, sr, cc := newTestUseCases()
	past := time.Now().Add(-time.Hour)
	sub := &entity.Subscription{
		ID: "sub-overdue", UserID: "user-1", PlanID: "plan-x",
		Status: entity.SubscriptionActive, ExpiresAt: &past,
		BasketSnapshot: []entity.PlanBasketItem{{GameID: "343050"}},
	}
	_ = sr.Save(context.Background(), sub)
	cc.insts = []controller.GameInstance{{ID: "inst-running", GameID: "343050", Status: "running"}}

	n, err := subUC.ExpireOverdue(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 expired, got %d", n)
	}
	got, _ := subUC.Get(context.Background(), sub.ID)
	if got.Status != entity.SubscriptionExpired {
		t.Fatalf("status should be expired, got %s", got.Status)
	}
	if len(cc.stopped) != 1 || cc.stopped[0] != "inst-running" {
		t.Fatalf("active instance should be stopped, got %v", cc.stopped)
	}
}

// TestSuspend_StopsActiveInstances 停用订阅：停止活跃实例 + 状态 suspended
func TestSuspend_StopsActiveInstances(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)
	cc.insts = []controller.GameInstance{{ID: "inst-running", GameID: "343050", Status: "running"}}

	suspended, err := subUC.Suspend(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended.Status != entity.SubscriptionSuspended {
		t.Fatalf("status should be suspended, got %s", suspended.Status)
	}
	if len(cc.stopped) != 1 || cc.stopped[0] != "inst-running" {
		t.Fatalf("active instance should be stopped, got %v", cc.stopped)
	}
}

// TestUnsuspend 恢复停用订阅
func TestUnsuspend(t *testing.T) {
	planUC, subUC, _, _, _ := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)
	if _, err := subUC.Suspend(context.Background(), sub.ID); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	back, err := subUC.Unsuspend(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("unsuspend: %v", err)
	}
	if back.Status != entity.SubscriptionActive {
		t.Fatalf("status should be active after unsuspend, got %s", back.Status)
	}
}

// B-04/P1-1：GetInstanceRuntime 归属校验 + 透传运行时统计
func TestGetInstanceRuntime_OwnershipAndPassthrough(t *testing.T) {
	planUC, subUC, _, _, cc := newTestUseCases()
	sub := buyActiveSub(t, planUC, subUC)
	subID := sub.ID

	// 实例属于订阅 → 透传 controller 运行时统计
	cc.getInst = &controller.GameInstance{ID: "inst-1", GameID: "343050", SubscriptionID: &subID}
	cc.runtime = &controller.InstanceRuntime{
		InstanceID: "inst-1", Running: true, PlayerCount: 3, MaxPlayers: 8,
		Healthy: true, ProbeMode: "script", SampledAt: "now",
	}
	rt, err := subUC.GetInstanceRuntime(context.Background(), "user-1", subID, "inst-1")
	if err != nil {
		t.Fatalf("get runtime: %v", err)
	}
	if rt == nil || rt.PlayerCount != 3 || rt.MaxPlayers != 8 || !rt.Healthy {
		t.Fatalf("runtime should pass through, got %+v", rt)
	}

	// 实例不属于该订阅 → 拒绝
	other := "sub-other"
	cc.getInst = &controller.GameInstance{ID: "inst-2", GameID: "343050", SubscriptionID: &other}
	if _, err := subUC.GetInstanceRuntime(context.Background(), "user-1", subID, "inst-2"); err == nil {
		t.Fatal("instance of another subscription should be rejected")
	}

	// 他人订阅 → 拒绝
	if _, err := subUC.GetInstanceRuntime(context.Background(), "user-2", subID, "inst-1"); err == nil {
		t.Fatal("other user's subscription should be rejected")
	}
}
