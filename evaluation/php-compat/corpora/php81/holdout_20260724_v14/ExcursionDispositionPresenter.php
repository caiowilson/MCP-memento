<?php

declare(strict_types=1);

namespace FieldOps\ColdChain\Presentation;

use FieldOps\ColdChain\Domain\ExcursionDisposition;

final class ExcursionDispositionPresenter
{
    /**
     * @return array{code: string, label: string, approval_required: bool}
     */
    public function present(ExcursionDisposition $disposition): array
    {
        return [
            'code' => $disposition->value,
            'label' => match ($disposition) {
                ExcursionDisposition::Quarantine => 'Hold inventory',
                ExcursionDisposition::Release => 'Release inventory',
                ExcursionDisposition::Destroy => 'Destroy inventory',
                ExcursionDisposition::ReturnToSupplier => 'Return to supplier',
            },
            'approval_required' => $disposition->requiresSupervisorApproval(),
        ];
    }
}
