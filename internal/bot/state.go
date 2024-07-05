package bot

type step string

const (
	stepCancel          step = "cancel"
	stepSelectKeyAmount step = "select_key_amount"

	stepApproveOrder step = "approve_order"
	stepRejectOrder  step = "reject_order"

	stepRenewOrder         step = "renew_order"
	stepRejectOrderRenewal step = "reject_order_renewal"
)

func (s step) String() string { return string(s) }

type State struct {
	step string
	data any
}

type order struct {
	keyAmount int8
	sum       int16
}
