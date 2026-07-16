<?php

declare(strict_types=1);

namespace LanternMuseum\Domain;

enum FumigationStage: string
{
    case SealedForTreatment = 'sealed_for_treatment';
    case ExposureUnderway = 'exposure_underway';
    case ClearedForStorage = 'cleared_for_storage';
}
