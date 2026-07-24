<?php

declare(strict_types=1);

namespace Compat\Php81\HoldoutV13\Membership;

enum MembershipFreezeCause: string
{
    case MemberRequest = 'member_request';
    case MedicalLeave = 'medical_leave';
    case BillingDispute = 'billing_dispute';
    case ComplianceReview = 'compliance_review';

    public function isMemberInitiated(): bool
    {
        return $this === self::MemberRequest
            || $this === self::MedicalLeave;
    }
}
