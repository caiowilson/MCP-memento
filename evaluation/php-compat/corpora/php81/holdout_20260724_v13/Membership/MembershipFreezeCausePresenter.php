<?php

declare(strict_types=1);

namespace Compat\Php81\HoldoutV13\Membership;

final class MembershipFreezeCausePresenter
{
    /**
     * @return array{code: string, label: string, memberInitiated: bool}
     */
    public function present(MembershipFreezeCause $cause): array
    {
        return [
            'code' => $cause->value,
            'label' => match ($cause) {
                MembershipFreezeCause::MemberRequest => 'Member request',
                MembershipFreezeCause::MedicalLeave => 'Medical leave',
                MembershipFreezeCause::BillingDispute => 'Billing dispute',
                MembershipFreezeCause::ComplianceReview => 'Compliance review',
            },
            'memberInitiated' => $cause->isMemberInitiated(),
        ];
    }
}
